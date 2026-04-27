package conntrack

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestSyslogWriter_FormatMessage(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	now := time.Now()
	conn := &Connection{
		SourceIP:      net.ParseIP("192.168.1.100"),
		SourcePort:    54321,
		DestIP:        net.ParseIP("8.8.8.8"),
		DestPort:      443,
		Protocol:      6,
		Direction:     DirectionOutgoing,
		State:         StateEstablished,
		PID:           1234,
		ProcessName:   "curl",
		Timestamp:     now,
		SynSentTime:   now,
		EstablishedTime: now.Add(50 * time.Millisecond),
	}

	msg := writer.formatMessage(conn, EventEstablished)

	// Check message format
	expectedParts := []string{
		"CONN_OUT_ESTABLISHED",
		"src=192.168.1.100:54321",
		"dst=8.8.8.8:443",
		"proto=TCP",
		"dir=outgoing",
		"state=ESTABLISHED",
		"pid=1234",
		`comm="curl"`,
		"handshake_ms=50",
	}

	for _, part := range expectedParts {
		if !strings.Contains(msg, part) {
			t.Errorf("Message missing expected part %q: %s", part, msg)
		}
	}
}

func TestSyslogWriter_FormatIncomingConnection(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	now := time.Now()
	conn := &Connection{
		SourceIP:    net.ParseIP("10.0.0.50"),
		SourcePort:  40000,
		DestIP:      net.ParseIP("192.168.1.100"),
		DestPort:    80,
		Protocol:    6,
		Direction:   DirectionIncoming,
		State:       StateEstablished,
		PID:         5678,
		ProcessName: "nginx",
		Timestamp:   now,
	}

	msg := writer.formatMessage(conn, EventEstablished)

	// Check message format
	expectedParts := []string{
		"CONN_IN_ACCEPTED",
		"src=10.0.0.50:40000",
		"dst=192.168.1.100:80",
		"proto=TCP",
		"dir=incoming",
		"state=ESTABLISHED",
		"pid=5678",
		`comm="nginx"`,
	}

	for _, part := range expectedParts {
		if !strings.Contains(msg, part) {
			t.Errorf("Message missing expected part %q: %s", part, msg)
		}
	}
}

func TestSyslogWriter_FormatClosedConnection(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	now := time.Now()
	conn := &Connection{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		State:       StateClosed,
		Timestamp:   now.Add(-120 * time.Second),
		ClosedTime:  now,
	}

	msg := writer.formatMessage(conn, EventClosed)

	// Check message format
	expectedParts := []string{
		"CONN_CLOSED",
		"src=192.168.1.100:54321",
		"dst=8.8.8.8:443",
		"proto=TCP",
		"duration_s=120.0",
	}

	for _, part := range expectedParts {
		if !strings.Contains(msg, part) {
			t.Errorf("Message missing expected part %q: %s", part, msg)
		}
	}
}

func TestSyslogWriter_FormatFailedConnection(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	now := time.Now()
	conn := &Connection{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		State:       StateClosed,
		PID:         1234,
		ProcessName: "curl",
		Timestamp:   now,
	}

	msg := writer.formatMessage(conn, EventFailed)

	// Check message format
	expectedParts := []string{
		"CONN_OUT_FAILED",
		"src=192.168.1.100:54321",
		"dst=8.8.8.8:443",
		"proto=TCP",
		"pid=1234",
		`comm="curl"`,
	}

	for _, part := range expectedParts {
		if !strings.Contains(msg, part) {
			t.Errorf("Message missing expected part %q: %s", part, msg)
		}
	}
}

func TestSyslogWriter_ProtocolString(t *testing.T) {
	cfg := SyslogConfig{
		Tag:      "conntrack-test",
		Facility: LOG_LOCAL0,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	tests := []struct {
		proto  uint8
		expect string
	}{
		{6, "TCP"},
		{17, "UDP"},
		{1, "ICMP"},
		{99, "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := writer.protocolString(tt.proto); got != tt.expect {
				t.Errorf("protocolString(%d) = %q, want %q", tt.proto, got, tt.expect)
			}
		})
	}
}

func TestSyslogWriter_WithHostname(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: true,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	conn := &Connection{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		State:       StateNew,
		Timestamp:   time.Now(),
	}

	msg := writer.formatMessage(conn, EventNew)

	if !strings.Contains(msg, "host=") {
		t.Errorf("Message should include hostname: %s", msg)
	}
}

func TestSyslogWriter_WithoutProcessName(t *testing.T) {
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		t.Skipf("Syslog not available: %v", err)
	}
	defer writer.Close()

	conn := &Connection{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		State:       StateNew,
		PID:         0,
		ProcessName: "",
		Timestamp:   time.Now(),
	}

	msg := writer.formatMessage(conn, EventNew)

	// Should not include pid or comm when not available
	if strings.Contains(msg, "pid=") {
		t.Errorf("Message should not include pid when not available: %s", msg)
	}
	if strings.Contains(msg, "comm=") {
		t.Errorf("Message should not include comm when not available: %s", msg)
	}
}
