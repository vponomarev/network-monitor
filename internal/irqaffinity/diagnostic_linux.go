//go:build linux

package irqaffinity

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type irqDiagnosticSample struct {
	counts      map[string]uint64
	total       uint64
	description string
}

func diagnose(ctx context.Context, options DiagnosticOptions) (DiagnosticReport, error) {
	if options.SysRoot == "" {
		options.SysRoot = "/sys"
	}
	if options.ProcRoot == "" {
		options.ProcRoot = "/proc"
	}
	if options.EtcRoot == "" {
		options.EtcRoot = "/etc"
	}
	if options.SampleDuration == 0 {
		options.SampleDuration = time.Second
	}

	report := DiagnosticReport{
		SchemaVersion: DiagnosticSchemaVersion,
		CollectedAt:   time.Now().UTC(),
		Architecture:  runtime.GOARCH,
		OSRelease:     readOSRelease(filepath.Join(options.EtcRoot, "os-release")),
	}
	report.Hostname, _ = os.Hostname()
	report.KernelRelease, _ = readOptionalString(filepath.Join(options.ProcRoot, "sys", "kernel", "osrelease"))
	report.KernelCmdline, _ = readOptionalString(filepath.Join(options.ProcRoot, "cmdline"))

	firstCPU, err := readCPUStats(filepath.Join(options.ProcRoot, "stat"))
	if err != nil {
		report.Warnings = append(report.Warnings, "reading initial CPU statistics: "+err.Error())
		firstCPU = map[int]cpuSample{}
	}
	firstIRQ, err := readIRQDiagnostics(filepath.Join(options.ProcRoot, "interrupts"))
	if err != nil {
		report.Warnings = append(report.Warnings, "reading initial interrupt statistics: "+err.Error())
		firstIRQ = map[int]irqDiagnosticSample{}
	}

	elapsed := options.SampleDuration
	if options.SampleDuration > 0 {
		timer := time.NewTimer(options.SampleDuration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-timer.C:
		}
	} else {
		elapsed = 0
	}

	secondCPU, err := readCPUStats(filepath.Join(options.ProcRoot, "stat"))
	if err != nil {
		report.Warnings = append(report.Warnings, "reading final CPU statistics: "+err.Error())
		secondCPU = firstCPU
	}
	cpuUtil := cpuUtilization(firstCPU, secondCPU)
	secondIRQ, err := readIRQDiagnostics(filepath.Join(options.ProcRoot, "interrupts"))
	if err != nil {
		report.Warnings = append(report.Warnings, "reading final interrupt statistics: "+err.Error())
		secondIRQ = firstIRQ
	}

	report.CPU = collectCPUDiagnostic(options, secondCPU, cpuUtil)
	report.Interfaces = collectInterfaceDiagnostics(options, firstIRQ, secondIRQ, elapsed, &report.Warnings)
	report.Softnet, err = readSoftnet(filepath.Join(options.ProcRoot, "net", "softnet_stat"))
	if err != nil {
		report.Warnings = append(report.Warnings, "reading softnet statistics: "+err.Error())
	}
	return report, nil
}

func collectCPUDiagnostic(options DiagnosticOptions, samples map[int]cpuSample, utilization map[int]float64) CPUDiagnostic {
	cpuRoot := filepath.Join(options.SysRoot, "devices", "system", "cpu")
	out := CPUDiagnostic{}
	out.OnlineCPUs, _ = readCPUList(filepath.Join(cpuRoot, "online"))
	out.IsolatedCPUs, _ = readCPUList(filepath.Join(cpuRoot, "isolated"))
	out.NoHzFullCPUs, _ = readCPUList(filepath.Join(cpuRoot, "nohz_full"))
	out.DefaultIRQAffinity, _ = readCPUMask(filepath.Join(options.ProcRoot, "irq", "default_smp_affinity"))
	if len(out.OnlineCPUs) == 0 {
		for cpu := range samples {
			out.OnlineCPUs = append(out.OnlineCPUs, cpu)
		}
		sort.Ints(out.OnlineCPUs)
	}
	for _, cpu := range out.OnlineCPUs {
		node, _ := cpuNUMA(options.SysRoot, cpu)
		core, _ := readInt(filepath.Join(cpuRoot, fmt.Sprintf("cpu%d", cpu), "topology", "core_id"))
		pkg, _ := readInt(filepath.Join(cpuRoot, fmt.Sprintf("cpu%d", cpu), "topology", "physical_package_id"))
		out.LogicalCPUs = append(out.LogicalCPUs, LogicalCPU{ID: cpu, NUMANode: node, CoreID: core, PhysicalPackage: pkg, UtilizationRatio: utilization[cpu]})
	}
	nodePaths, _ := filepath.Glob(filepath.Join(options.SysRoot, "devices", "system", "node", "node*"))
	for _, path := range nodePaths {
		id, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "node"))
		if err != nil {
			continue
		}
		cpus, _ := readCPUList(filepath.Join(path, "cpulist"))
		out.NUMANodes = append(out.NUMANodes, NUMANodeDiagnostic{ID: id, CPUs: cpus})
	}
	sort.Slice(out.NUMANodes, func(i, j int) bool { return out.NUMANodes[i].ID < out.NUMANodes[j].ID })
	return out
}

func collectInterfaceDiagnostics(options DiagnosticOptions, first, second map[int]irqDiagnosticSample, elapsed time.Duration, warnings *[]string) []InterfaceDiagnostic {
	netRoot := filepath.Join(options.SysRoot, "class", "net")
	entries, err := os.ReadDir(netRoot)
	if err != nil {
		*warnings = append(*warnings, "reading network interfaces: "+err.Error())
		return nil
	}
	interfaces := make([]InterfaceDiagnostic, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		base := filepath.Join(netRoot, name)
		item := InterfaceDiagnostic{Name: name, NUMANode: -1, Statistics: map[string]uint64{}}
		item.MACAddress, _ = readOptionalString(filepath.Join(base, "address"))
		item.OperState, _ = readOptionalString(filepath.Join(base, "operstate"))
		item.Carrier, _ = readOptionalString(filepath.Join(base, "carrier"))
		item.MTU, _ = readInt(filepath.Join(base, "mtu"))
		item.SpeedMbps, _ = readInt(filepath.Join(base, "speed"))
		item.Duplex, _ = readOptionalString(filepath.Join(base, "duplex"))
		item.Queues = collectQueueDiagnostics(filepath.Join(base, "queues"))
		item.Statistics = readUintDirectory(filepath.Join(base, "statistics"))

		device := filepath.Join(base, "device")
		if resolved, resolveErr := filepath.EvalSymlinks(device); resolveErr == nil {
			item.PCIAddress = filepath.Base(resolved)
		}
		if driver, driverErr := filepath.EvalSymlinks(filepath.Join(device, "driver")); driverErr == nil {
			item.Driver = filepath.Base(driver)
		}
		if numa, numaErr := readInt(filepath.Join(device, "numa_node")); numaErr == nil {
			item.NUMANode = numa
		} else if _, statErr := os.Stat(device); statErr == nil {
			item.Warnings = append(item.Warnings, "device NUMA node is unavailable")
		}
		item.LocalCPUs, _ = readCPUList(filepath.Join(device, "local_cpulist"))
		item.PCIVendor, _ = readOptionalString(filepath.Join(device, "vendor"))
		item.PCIDevice, _ = readOptionalString(filepath.Join(device, "device"))
		item.PCIClass, _ = readOptionalString(filepath.Join(device, "class"))

		irqs, irqErr := numericEntries(filepath.Join(device, "msi_irqs"))
		if irqErr != nil && item.PCIAddress != "" {
			item.Warnings = append(item.Warnings, "MSI-X IRQ list is unavailable")
		}
		for _, irq := range irqs {
			sample := second[irq]
			irqItem := IRQDiagnostic{IRQ: irq, Description: sample.description, CountsByCPU: sample.counts, TotalInterrupts: sample.total}
			if before, ok := first[irq]; ok && sample.total >= before.total && elapsed > 0 {
				irqItem.InterruptsPerSecond = float64(sample.total-before.total) / elapsed.Seconds()
			}
			irqItem.EffectiveAffinity, _ = readCPUList(filepath.Join(options.ProcRoot, "irq", strconv.Itoa(irq), "effective_affinity_list"))
			irqItem.ConfiguredAffinity, _ = readCPUList(filepath.Join(options.ProcRoot, "irq", strconv.Itoa(irq), "smp_affinity_list"))
			irqItem.AffinityHint, _ = readCPUMask(filepath.Join(options.ProcRoot, "irq", strconv.Itoa(irq), "affinity_hint"))
			if len(irqItem.EffectiveAffinity) == 0 {
				irqItem.EffectiveAffinity = append([]int(nil), irqItem.ConfiguredAffinity...)
			}
			nodeSet := map[int]struct{}{}
			for _, cpu := range irqItem.EffectiveAffinity {
				node, _ := cpuNUMA(options.SysRoot, cpu)
				if node >= 0 {
					nodeSet[node] = struct{}{}
					if item.NUMANode >= 0 && node != item.NUMANode {
						irqItem.CrossNUMA = true
					}
				}
			}
			for node := range nodeSet {
				irqItem.TargetNUMANodes = append(irqItem.TargetNUMANodes, node)
			}
			sort.Ints(irqItem.TargetNUMANodes)
			item.IRQs = append(item.IRQs, irqItem)
		}
		interfaces = append(interfaces, item)
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	return interfaces
}

func readIRQDiagnostics(path string) (map[int]irqDiagnosticSample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var cpuNames []string
	out := map[int]irqDiagnosticSample{}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(cpuNames) == 0 && strings.HasPrefix(fields[0], "CPU") {
			for _, field := range fields {
				if strings.HasPrefix(field, "CPU") {
					cpuNames = append(cpuNames, field)
				}
			}
			continue
		}
		irqText := strings.TrimSuffix(fields[0], ":")
		irq, parseErr := strconv.Atoi(irqText)
		if parseErr != nil {
			continue
		}
		sample := irqDiagnosticSample{counts: map[string]uint64{}}
		index := 1
		for _, cpuName := range cpuNames {
			if index >= len(fields) {
				break
			}
			value, valueErr := strconv.ParseUint(fields[index], 10, 64)
			if valueErr != nil {
				break
			}
			sample.counts[cpuName] = value
			sample.total += value
			index++
		}
		if index < len(fields) {
			sample.description = strings.Join(fields[index:], " ")
		}
		out[irq] = sample
	}
	return out, scanner.Err()
}

func readCPUMask(path string) ([]int, error) {
	text, err := readOptionalString(path)
	if err != nil {
		return nil, err
	}
	chunks := strings.Split(text, ",")
	var cpus []int
	for reverseIndex := 0; reverseIndex < len(chunks); reverseIndex++ {
		chunk := chunks[len(chunks)-1-reverseIndex]
		value, parseErr := strconv.ParseUint(chunk, 16, 32)
		if parseErr != nil {
			return nil, parseErr
		}
		for bit := 0; bit < 32; bit++ {
			if value&(uint64(1)<<bit) != 0 {
				cpus = append(cpus, reverseIndex*32+bit)
			}
		}
	}
	return cpus, nil
}

func readSoftnet(path string) ([]SoftnetDiagnostic, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []SoftnetDiagnostic
	scanner := bufio.NewScanner(f)
	cpu := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		values := make([]uint64, len(fields))
		for i, field := range fields {
			values[i], _ = strconv.ParseUint(field, 16, 64)
		}
		valueAt := func(index int) uint64 {
			if index < len(values) {
				return values[index]
			}
			return 0
		}
		out = append(out, SoftnetDiagnostic{CPU: cpu, Processed: valueAt(0), Dropped: valueAt(1), TimeSqueeze: valueAt(2), CPUCollision: valueAt(8), ReceivedRPS: valueAt(9), FlowLimitCount: valueAt(10)})
		cpu++
	}
	return out, scanner.Err()
}

func readOSRelease(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			out[key] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	return out
}

func readOptionalString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func entryNames(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

func collectQueueDiagnostics(path string) []QueueDiagnostic {
	names := entryNames(path)
	out := make([]QueueDiagnostic, 0, len(names))
	for _, name := range names {
		base := filepath.Join(path, name)
		queue := QueueDiagnostic{Name: name}
		queue.RPSCPUs, _ = readCPUMask(filepath.Join(base, "rps_cpus"))
		queue.XPSCPUs, _ = readCPUMask(filepath.Join(base, "xps_cpus"))
		queue.RPSFlowCount, _ = readUint(filepath.Join(base, "rps_flow_cnt"))
		out = append(out, queue)
	}
	return out
}

func readUintDirectory(path string) map[string]uint64 {
	out := map[string]uint64{}
	for _, name := range entryNames(path) {
		if value, err := readUint(filepath.Join(path, name)); err == nil {
			out[name] = value
		}
	}
	return out
}
