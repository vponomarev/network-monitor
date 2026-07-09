//go:build linux
// +build linux

package losscollector

import (
	"encoding/binary"
	"testing"

	"go.uber.org/zap"
)

type fakeExporter struct {
	calls [][2]string
}

func (f *fakeExporter) RecordRetransmit(srcIP, dstIP string) {
	f.calls = append(f.calls, [2]string{srcIP, dstIP})
}

func TestValidateBpfLossEvent(t *testing.T) {
	if err := validateBpfLossEvent(); err != nil {
		t.Fatalf("layout mismatch: %v", err)
	}
}

// buildEvent constructs a 48-byte tcploss_event matching bpf/tcploss.bpf.c.
func buildEvent(src, dst [4]byte, sport, dport uint16) []byte {
	b := make([]byte, 48)
	binary.LittleEndian.PutUint64(b[0:8], 123456789)
	// src_ip IPv4-mapped
	b[8+10], b[8+11] = 0xff, 0xff
	copy(b[8+12:8+16], src[:])
	// dst_ip IPv4-mapped
	b[24+10], b[24+11] = 0xff, 0xff
	copy(b[24+12:24+16], dst[:])
	binary.LittleEndian.PutUint16(b[40:42], sport)
	binary.LittleEndian.PutUint16(b[42:44], dport)
	b[44] = afInet
	return b
}

func TestHandleRecord_ParsesEvent(t *testing.T) {
	fe := &fakeExporter{}
	c := &EBPFLossCollector{exporter: fe, logger: zap.NewNop()}

	c.handleRecord(buildEvent([4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2}, 1234, 443))

	if len(fe.calls) != 1 {
		t.Fatalf("expected 1 RecordRetransmit call, got %d", len(fe.calls))
	}
	if fe.calls[0] != [2]string{"10.0.0.1", "10.0.0.2"} {
		t.Fatalf("wrong IPs: %v", fe.calls[0])
	}
	if c.EventsParsed() != 1 {
		t.Fatalf("EventsParsed=%d, want 1", c.EventsParsed())
	}
}

func TestHandleRecord_ShortEvent(t *testing.T) {
	fe := &fakeExporter{}
	c := &EBPFLossCollector{exporter: fe, logger: zap.NewNop()}

	c.handleRecord(make([]byte, 10)) // too short

	if len(fe.calls) != 0 {
		t.Fatalf("short event must not call exporter")
	}
	if c.ParseErrors() != 1 {
		t.Fatalf("ParseErrors=%d, want 1", c.ParseErrors())
	}
}

func TestHandleRecord_NonIPv4Skipped(t *testing.T) {
	fe := &fakeExporter{}
	c := &EBPFLossCollector{exporter: fe, logger: zap.NewNop()}

	ev := buildEvent([4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2}, 1, 2)
	ev[44] = 10 // AF_INET6
	c.handleRecord(ev)

	if len(fe.calls) != 0 {
		t.Fatalf("non-IPv4 must not call exporter")
	}
}
