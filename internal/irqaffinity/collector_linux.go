//go:build linux

package irqaffinity

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type Options struct {
	Interval      time.Duration
	BusyThreshold float64
	SysRoot       string
	ProcRoot      string
}

type cpuSample struct{ total, idle uint64 }

type Collector struct {
	options                                        Options
	logger                                         *zap.Logger
	prevCPU                                        map[int]cpuSample
	prevDrop                                       map[string]uint64
	prevIRQ                                        map[int]uint64
	prevAt                                         time.Time
	info, crossNUMA, irqBusy, risk, anomaly, drops *prometheus.GaugeVec
	irqRate                                        *prometheus.GaugeVec
	up                                             prometheus.Gauge
	monitored                                      prometheus.Gauge
}

func New(options Options, logger *zap.Logger, reg prometheus.Registerer) *Collector {
	if options.Interval <= 0 {
		options.Interval = 15 * time.Second
	}
	if options.BusyThreshold <= 0 || options.BusyThreshold > 1 {
		options.BusyThreshold = 0.80
	}
	if options.SysRoot == "" {
		options.SysRoot = "/sys"
	}
	if options.ProcRoot == "" {
		options.ProcRoot = "/proc"
	}
	c := &Collector{options: options, logger: logger.Named("irq_affinity"), prevCPU: map[int]cpuSample{}, prevDrop: map[string]uint64{}, prevIRQ: map[int]uint64{},
		info:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_info", Help: "NIC NUMA placement information."}, []string{"interface", "nic_numa"}),
		crossNUMA: prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_cross_numa", Help: "Whether an IRQ targets CPUs outside the NIC NUMA node."}, []string{"interface", "irq"}),
		irqBusy:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_target_cpu_utilization_ratio", Help: "Maximum utilization of CPUs targeted by an IRQ."}, []string{"interface", "irq"}),
		risk:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_risk", Help: "Active cross-NUMA IRQ placement combined with busy remote target CPUs."}, []string{"interface"}),
		anomaly:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_packet_loss_anomaly", Help: "IRQ affinity risk correlated with increasing receive drops."}, []string{"interface"}),
		drops:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_rx_drop_counter", Help: "Kernel-reported absolute receive drop counter used for IRQ correlation."}, []string{"interface", "type"}),
		up:        prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_collector_up", Help: "Whether the last IRQ affinity collection succeeded."}),
		monitored: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_monitored_interfaces", Help: "Number of NICs with discoverable NUMA and MSI-X IRQ data."}),
		irqRate:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "netmon", Name: "irq_affinity_interrupts_per_second", Help: "Approximate interrupt rate for a NIC IRQ."}, []string{"interface", "irq"}),
	}
	if reg != nil {
		reg.MustRegister(c.info, c.crossNUMA, c.irqBusy, c.risk, c.anomaly, c.drops, c.irqRate, c.up, c.monitored)
	}
	return c
}

func (c *Collector) Run(ctx context.Context) error {
	if err := c.collect(); err != nil {
		c.up.Set(0)
		c.logger.Warn("Initial IRQ affinity collection failed", zap.Error(err))
	}
	ticker := time.NewTicker(c.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.collect(); err != nil {
				c.up.Set(0)
				c.logger.Warn("IRQ affinity collection failed", zap.Error(err))
			}
		}
	}
}

func (c *Collector) collect() error {
	cpuNow, err := readCPUStats(filepath.Join(c.options.ProcRoot, "stat"))
	if err != nil {
		return err
	}
	util := cpuUtilization(c.prevCPU, cpuNow)
	c.prevCPU = cpuNow
	irqNow, err := readIRQCounts(filepath.Join(c.options.ProcRoot, "interrupts"))
	if err != nil {
		return fmt.Errorf("reading interrupts: %w", err)
	}
	now := time.Now()
	elapsed := c.options.Interval.Seconds()
	if !c.prevAt.IsZero() && now.After(c.prevAt) {
		elapsed = now.Sub(c.prevAt).Seconds()
	}
	netRoot := filepath.Join(c.options.SysRoot, "class", "net")
	interfaces, err := os.ReadDir(netRoot)
	if err != nil {
		return fmt.Errorf("reading interfaces: %w", err)
	}
	c.info.Reset()
	c.crossNUMA.Reset()
	c.irqBusy.Reset()
	c.risk.Reset()
	c.anomaly.Reset()
	c.drops.Reset()
	c.irqRate.Reset()
	monitored := 0
	for _, entry := range interfaces {
		name := entry.Name()
		device := filepath.Join(netRoot, name, "device")
		numa, err := readInt(filepath.Join(device, "numa_node"))
		if err != nil || numa < 0 {
			continue
		}
		irqs, err := numericEntries(filepath.Join(device, "msi_irqs"))
		if err != nil || len(irqs) == 0 {
			continue
		}
		monitored++
		c.info.WithLabelValues(name, strconv.Itoa(numa)).Set(1)
		interfaceRisk := false
		for _, irq := range irqs {
			affinityPath := filepath.Join(c.options.ProcRoot, "irq", strconv.Itoa(irq), "effective_affinity_list")
			cpus, err := readCPUList(affinityPath)
			if err != nil {
				cpus, _ = readCPUList(filepath.Join(c.options.ProcRoot, "irq", strconv.Itoa(irq), "smp_affinity_list"))
			}
			cross := false
			maxBusy := float64(0)
			maxRemoteBusy := float64(0)
			for _, cpu := range cpus {
				node, _ := cpuNUMA(c.options.SysRoot, cpu)
				if node >= 0 && node != numa {
					cross = true
					if util[cpu] > maxRemoteBusy {
						maxRemoteBusy = util[cpu]
					}
				}
				if util[cpu] > maxBusy {
					maxBusy = util[cpu]
				}
			}
			irqLabel := strconv.Itoa(irq)
			rate := float64(0)
			if previous, ok := c.prevIRQ[irq]; ok && irqNow[irq] >= previous {
				rate = float64(irqNow[irq]-previous) / elapsed
			}
			c.irqRate.WithLabelValues(name, irqLabel).Set(rate)
			c.crossNUMA.WithLabelValues(name, irqLabel).Set(boolFloat(cross))
			c.irqBusy.WithLabelValues(name, irqLabel).Set(maxBusy)
			if cross && maxRemoteBusy >= c.options.BusyThreshold && rate > 0 {
				interfaceRisk = true
			}
		}
		dropIncreased := false
		for _, kind := range []string{"rx_dropped", "rx_missed_errors", "rx_nohandler"} {
			value, err := readUint(filepath.Join(netRoot, name, "statistics", kind))
			if err != nil {
				continue
			}
			key := name + "\x00" + kind
			if previous, ok := c.prevDrop[key]; ok && value > previous {
				dropIncreased = true
			}
			c.prevDrop[key] = value
			c.drops.WithLabelValues(name, kind).Set(float64(value))
		}
		c.risk.WithLabelValues(name).Set(boolFloat(interfaceRisk))
		c.anomaly.WithLabelValues(name).Set(boolFloat(interfaceRisk && dropIncreased))
	}
	c.prevIRQ = irqNow
	c.prevAt = now
	c.monitored.Set(float64(monitored))
	c.up.Set(1)
	return nil
}

func readCPUStats(path string) (map[int]cpuSample, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	out := map[int]cpuSample{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") || fields[0] == "cpu" {
			continue
		}
		id, e := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
		if e != nil {
			continue
		}
		var total, idle uint64
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 || i == 5 {
				idle += v
			}
		}
		out[id] = cpuSample{total, idle}
	}
	return out, s.Err()
}

func readIRQCounts(path string) (map[int]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[int]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			continue
		}
		irq, err := strconv.Atoi(strings.TrimSpace(line[:colon]))
		if err != nil {
			continue
		}
		var total uint64
		for _, field := range strings.Fields(line[colon+1:]) {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				break
			}
			total += value
		}
		out[irq] = total
	}
	return out, scanner.Err()
}

func cpuUtilization(previous, current map[int]cpuSample) map[int]float64 {
	out := map[int]float64{}
	for cpu, now := range current {
		old, ok := previous[cpu]
		if !ok || now.total <= old.total {
			continue
		}
		dt := now.total - old.total
		di := now.idle - old.idle
		if di > dt {
			di = dt
		}
		out[cpu] = float64(dt-di) / float64(dt)
	}
	return out
}
func numericEntries(path string) ([]int, error) {
	entries, e := os.ReadDir(path)
	if e != nil {
		return nil, e
	}
	var out []int
	for _, x := range entries {
		v, e := strconv.Atoi(x.Name())
		if e == nil {
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out, nil
}
func readCPUList(path string) ([]int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var out []int
	for _, part := range strings.Split(strings.TrimSpace(string(b)), ",") {
		bounds := strings.SplitN(part, "-", 2)
		start, e := strconv.Atoi(bounds[0])
		if e != nil {
			return nil, e
		}
		end := start
		if len(bounds) == 2 {
			end, e = strconv.Atoi(bounds[1])
			if e != nil {
				return nil, e
			}
		}
		for i := start; i <= end; i++ {
			out = append(out, i)
		}
	}
	return out, nil
}
func cpuNUMA(sysRoot string, cpu int) (int, error) {
	matches, e := filepath.Glob(filepath.Join(sysRoot, "devices", "system", "cpu", fmt.Sprintf("cpu%d", cpu), "node*"))
	if e != nil || len(matches) == 0 {
		return -1, fmt.Errorf("NUMA node unavailable")
	}
	return strconv.Atoi(strings.TrimPrefix(filepath.Base(matches[0]), "node"))
}
func readInt(path string) (int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
func readUint(path string) (uint64, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}
func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
