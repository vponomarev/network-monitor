package collector

import (
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

// stubExporter is a no-op RetransmitExporter for tests.
type stubExporter struct{}

func (stubExporter) RecordRetransmit(srcIP, dstIP string) {}

func TestSignalReady_CalledOnce(t *testing.T) {
	c := NewTracePipeCollector("/dev/null", stubExporter{}, zap.NewNop(), nil)

	var calls atomic.Int64
	c.SetReadyFunc(func() { calls.Add(1) })

	// Multiple opens of trace_pipe must signal readiness only once.
	c.signalReady()
	c.signalReady()
	c.signalReady()

	if got := calls.Load(); got != 1 {
		t.Fatalf("onReady should be called exactly once, got %d", got)
	}
}

func TestSignalReady_NoCallbackNoPanic(t *testing.T) {
	c := NewTracePipeCollector("/dev/null", stubExporter{}, zap.NewNop(), nil)
	// No SetReadyFunc — must not panic.
	c.signalReady()
}
