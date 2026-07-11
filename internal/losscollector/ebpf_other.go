//go:build !linux
// +build !linux

// Package losscollector: non-Linux stub. The eBPF loss collector is Linux-only;
// this build lets the tree compile on other platforms (e.g. macOS dev machines).
package losscollector

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RetransmitExporter mirrors the Linux interface.
type RetransmitExporter interface {
	RecordRetransmit(srcIP, dstIP string)
}

// Metrics mirrors the Linux interface.
type Metrics interface {
	IncEventsRead()
	IncEventsParsed()
	IncParseErrors()
	AddEventsDropped(reason string, count uint64)
}

// Options mirrors the Linux type.
type Options struct {
	ProgramPath string
}

// EBPFLossCollector is a non-functional stub on non-Linux platforms.
type EBPFLossCollector struct {
	logger *zap.Logger
}

// NewEBPFLossCollector returns a stub collector.
func NewEBPFLossCollector(exporter RetransmitExporter, logger *zap.Logger, opts Options) (*EBPFLossCollector, error) {
	return &EBPFLossCollector{logger: logger.Named("losscollector")}, nil
}

// SetReadyFunc is a no-op on non-Linux platforms.
func (c *EBPFLossCollector) SetReadyFunc(f func()) {}

// SetMetrics is a no-op on non-Linux platforms.
func (c *EBPFLossCollector) SetMetrics(m Metrics) {}

// Run returns an error: the eBPF loss collector requires Linux.
func (c *EBPFLossCollector) Run(ctx context.Context) error {
	return fmt.Errorf("eBPF loss collector is only supported on Linux")
}

// Close is a no-op on non-Linux platforms.
func (c *EBPFLossCollector) Close() error { return nil }

// EventsRead always returns 0 on non-Linux platforms.
func (c *EBPFLossCollector) EventsRead() uint64 { return 0 }

// EventsParsed always returns 0 on non-Linux platforms.
func (c *EBPFLossCollector) EventsParsed() uint64 { return 0 }

// ParseErrors always returns 0 on non-Linux platforms.
func (c *EBPFLossCollector) ParseErrors() uint64 { return 0 }
