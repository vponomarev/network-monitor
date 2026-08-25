//go:build linux
// +build linux

// Package losscollector collects TCP loss (retransmit) events from an eBPF
// tracepoint program via a ring buffer. It is the production replacement for
// the legacy trace_pipe text scraper (internal/collector).
package losscollector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vponomarev/network-monitor/pkg/embedded"
	"go.uber.org/zap"
)

// Names must match bpf/tcploss.bpf.c.
const (
	bpfProgName    = "handle_tcp_retransmit"
	bpfMapName     = "loss_events"
	bpfDropMapName = "loss_drops"
	afInet         = 2
)

// RetransmitExporter receives one call per retransmit event. Satisfied by
// metrics.Exporter (same contract as internal/collector).
type RetransmitExporter interface {
	RecordRetransmit(srcIP, dstIP string)
}

// Metrics receives self-observation counter updates. Satisfied structurally by
// *collector.CollectorMetrics (no import needed — duck typing).
type Metrics interface {
	IncEventsRead()
	IncEventsParsed()
	IncParseErrors()
	AddEventsDropped(reason string, count uint64)
}

// bpfLossEvent MUST byte-match struct tcploss_event in bpf/tcploss.bpf.c (48 bytes).
type bpfLossEvent struct {
	TimestampNs uint64   // offset 0
	SrcIP       [16]byte // offset 8
	DstIP       [16]byte // offset 24
	SrcPort     uint16   // offset 40
	DstPort     uint16   // offset 42
	Family      uint8    // offset 44
	_           [3]byte  // offset 45..47
}

// validateBpfLossEvent ensures the Go struct matches the C struct layout.
func validateBpfLossEvent() error {
	if got := unsafe.Sizeof(bpfLossEvent{}); got != 48 {
		return fmt.Errorf("bpfLossEvent size mismatch: got %d, expected 48", got)
	}
	if got := unsafe.Offsetof(bpfLossEvent{}.SrcIP); got != 8 {
		return fmt.Errorf("bpfLossEvent.SrcIP offset mismatch: got %d, expected 8", got)
	}
	if got := unsafe.Offsetof(bpfLossEvent{}.SrcPort); got != 40 {
		return fmt.Errorf("bpfLossEvent.SrcPort offset mismatch: got %d, expected 40", got)
	}
	return nil
}

// Options configures the collector.
type Options struct {
	// ProgramPath, if set, loads the eBPF object from this file instead of the
	// embedded copy (for debugging).
	ProgramPath string
}

// EBPFLossCollector loads the tcploss eBPF program, attaches the
// tcp_retransmit_skb tracepoint and forwards events to the exporter.
type EBPFLossCollector struct {
	exporter RetransmitExporter
	logger   *zap.Logger
	opts     Options

	coll *ebpf.Collection
	lnk  link.Link

	onReady   func()
	readyOnce sync.Once

	// Optional Prometheus self-metrics sink (nil-safe).
	metrics Metrics

	// Self-observation counters (also exposed via getters for tests).
	eventsRead   atomic.Uint64
	eventsParsed atomic.Uint64
	parseErrors  atomic.Uint64
}

// SetMetrics attaches a Prometheus self-metrics sink. Safe to call before Run.
func (c *EBPFLossCollector) SetMetrics(m Metrics) { c.metrics = m }

// NewEBPFLossCollector validates the event layout and returns a collector.
func NewEBPFLossCollector(exporter RetransmitExporter, logger *zap.Logger, opts Options) (*EBPFLossCollector, error) {
	if err := validateBpfLossEvent(); err != nil {
		return nil, fmt.Errorf("validating bpfLossEvent: %w", err)
	}
	return &EBPFLossCollector{
		exporter: exporter,
		logger:   logger.Named("losscollector"),
		opts:     opts,
	}, nil
}

// SetReadyFunc registers a callback invoked once, after the ring buffer reader
// is up and the collector is consuming events. Safe to call before Run.
func (c *EBPFLossCollector) SetReadyFunc(f func()) { c.onReady = f }

func (c *EBPFLossCollector) signalReady() {
	if c.onReady == nil {
		return
	}
	c.readyOnce.Do(c.onReady)
}

// EventsRead returns the number of ring buffer records read.
func (c *EBPFLossCollector) EventsRead() uint64 { return c.eventsRead.Load() }

// EventsParsed returns the number of events successfully forwarded to the exporter.
func (c *EBPFLossCollector) EventsParsed() uint64 { return c.eventsParsed.Load() }

// ParseErrors returns the number of malformed/short events.
func (c *EBPFLossCollector) ParseErrors() uint64 { return c.parseErrors.Load() }

// loadSpec loads the eBPF collection spec from the configured source.
func (c *EBPFLossCollector) loadSpec() (*ebpf.CollectionSpec, error) {
	if c.opts.ProgramPath != "" {
		c.logger.Info("Loading tcploss eBPF from file", zap.String("path", c.opts.ProgramPath))
		return ebpf.LoadCollectionSpec(c.opts.ProgramPath)
	}
	data, err := embedded.GetTCPLossEBPFProgram()
	if err != nil {
		return nil, fmt.Errorf("getting embedded tcploss eBPF: %w", err)
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("loading embedded tcploss spec: %w", err)
	}
	return spec, nil
}

// Run loads and attaches the program, then reads events until ctx is cancelled.
func (c *EBPFLossCollector) Run(ctx context.Context) error {
	c.logger.Info("Starting eBPF loss collector")

	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	spec, err := c.loadSpec()
	if err != nil {
		return err
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("creating eBPF collection: %w", err)
	}
	c.coll = coll
	defer c.close()

	prog, ok := coll.Programs[bpfProgName]
	if !ok {
		return fmt.Errorf("program %q not found in eBPF collection", bpfProgName)
	}

	lnk, err := link.Tracepoint("tcp", "tcp_retransmit_skb", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching tracepoint tcp/tcp_retransmit_skb: %w", err)
	}
	c.lnk = lnk
	c.logger.Info("Attached tracepoint tcp/tcp_retransmit_skb")

	rbMap, ok := coll.Maps[bpfMapName]
	if !ok {
		return fmt.Errorf("map %q not found in eBPF collection", bpfMapName)
	}
	rd, err := ringbuf.NewReader(rbMap)
	if err != nil {
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}
	defer rd.Close()

	// Close the reader on cancellation so the blocking Read returns ErrClosed.
	go func() {
		<-ctx.Done()
		rd.Close()
	}()
	if dropMap, ok := coll.Maps[bpfDropMapName]; ok {
		go c.observeKernelDrops(ctx, dropMap)
	} else {
		// Keep locally built binaries with an older embedded object usable. CI and
		// release always rebuild from bpf/tcploss.bpf.c and therefore include it.
		c.logger.Warn("eBPF drop counter map is unavailable; rebuild embedded tcploss.bpf.o",
			zap.String("map", bpfDropMapName))
	}

	c.signalReady()
	c.logger.Info("eBPF loss collector consuming events")

	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || errors.Is(err, os.ErrClosed) {
				c.logger.Info("Ring buffer closed, stopping loss collector")
				return nil
			}
			c.logger.Warn("Reading loss ring buffer", zap.Error(err))
			// Back off to avoid a busy loop on persistent errors.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		c.eventsRead.Add(1)
		if c.metrics != nil {
			c.metrics.IncEventsRead()
		}
		c.handleRecord(record.RawSample)
	}
}

// observeKernelDrops converts the absolute per-CPU eBPF counter into deltas for
// Prometheus. A read failure affects observability, not event collection.
func (c *EBPFLossCollector) observeKernelDrops(ctx context.Context, dropMap *ebpf.Map) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var previous uint64
	collect := func() {
		current, err := readPerCPUDropCount(dropMap)
		if err != nil {
			c.logger.Warn("Reading eBPF loss drop counter", zap.Error(err))
			return
		}
		if current >= previous && c.metrics != nil {
			c.metrics.AddEventsDropped("ringbuf_full", current-previous)
		}
		previous = current
	}

	collect()
	for {
		select {
		case <-ctx.Done():
			collect()
			return
		case <-ticker.C:
			collect()
		}
	}
}

func readPerCPUDropCount(dropMap *ebpf.Map) (uint64, error) {
	key := uint32(0)
	var perCPU []uint64
	if err := dropMap.Lookup(&key, &perCPU); err != nil {
		return 0, err
	}
	var total uint64
	for _, value := range perCPU {
		total += value
	}
	return total, nil
}

// handleRecord parses one raw event and forwards it to the exporter.
func (c *EBPFLossCollector) handleRecord(raw []byte) {
	if len(raw) < 48 {
		c.parseErrors.Add(1)
		if c.metrics != nil {
			c.metrics.IncParseErrors()
		}
		c.logger.Debug("Loss event too short", zap.Int("len", len(raw)))
		return
	}

	var evt bpfLossEvent
	evt.TimestampNs = binary.NativeEndian.Uint64(raw[0:8])
	copy(evt.SrcIP[:], raw[8:24])
	copy(evt.DstIP[:], raw[24:40])
	evt.SrcPort = binary.NativeEndian.Uint16(raw[40:42])
	evt.DstPort = binary.NativeEndian.Uint16(raw[42:44])
	evt.Family = raw[44]

	if evt.Family != afInet {
		// Non-IPv4 should be filtered in eBPF; skip defensively.
		c.parseErrors.Add(1)
		if c.metrics != nil {
			c.metrics.IncParseErrors()
		}
		return
	}

	srcIP := ipv4String(evt.SrcIP)
	dstIP := ipv4String(evt.DstIP)

	c.logger.Debug("Retransmit event",
		zap.String("src", srcIP),
		zap.String("dst", dstIP),
		zap.Uint16("sport", evt.SrcPort),
		zap.Uint16("dport", evt.DstPort))

	c.exporter.RecordRetransmit(srcIP, dstIP)
	c.eventsParsed.Add(1)
	if c.metrics != nil {
		c.metrics.IncEventsParsed()
	}
}

// ipv4String renders an IPv4-mapped 16-byte address as dotted-quad.
func ipv4String(b [16]byte) string {
	ip := net.IP(b[:])
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

// close releases eBPF resources.
func (c *EBPFLossCollector) close() {
	if c.lnk != nil {
		c.lnk.Close()
		c.lnk = nil
	}
	if c.coll != nil {
		c.coll.Close()
		c.coll = nil
	}
}

// Close stops the collector's resources (idempotent).
func (c *EBPFLossCollector) Close() error {
	c.close()
	return nil
}
