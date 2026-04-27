package conntrack

import (
	"fmt"
	"log/syslog"
	"net"
	"os"
	"time"
)

// SyslogFacility represents syslog facility
type SyslogFacility int

const (
	// LOG_USER is user-level messages
	LOG_USER SyslogFacility = 8
	// LOG_DAEMON is daemon/facility
	LOG_DAEMON SyslogFacility = 24
	// LOG_LOCAL0-7 are local facilities
	LOG_LOCAL0 SyslogFacility = 128
	LOG_LOCAL1 SyslogFacility = 136
	LOG_LOCAL2 SyslogFacility = 144
	LOG_LOCAL3 SyslogFacility = 152
	LOG_LOCAL4 SyslogFacility = 160
	LOG_LOCAL5 SyslogFacility = 168
	LOG_LOCAL6 SyslogFacility = 176
	LOG_LOCAL7 SyslogFacility = 184
)

// SyslogPriority represents syslog priority
type SyslogPriority int

const (
	// LOG_EMERG - system is unusable
	LOG_EMERG SyslogPriority = 0
	// LOG_ALERT - action must be taken immediately
	LOG_ALERT SyslogPriority = 1
	// LOG_CRIT - critical conditions
	LOG_CRIT SyslogPriority = 2
	// LOG_ERR - error conditions
	LOG_ERR SyslogPriority = 3
	// LOG_WARNING - warning conditions
	LOG_WARNING SyslogPriority = 4
	// LOG_NOTICE - normal but significant condition
	LOG_NOTICE SyslogPriority = 5
	// LOG_INFO - informational messages
	LOG_INFO SyslogPriority = 6
	// LOG_DEBUG - debug-level messages
	LOG_DEBUG SyslogPriority = 7
)

// SyslogConfig holds syslog writer configuration
type SyslogConfig struct {
	// Network type (empty for local syslog)
	Network string
	// Address (empty for local syslog, e.g., "localhost:514" for remote)
	Address string
	// Tag/program name
	Tag string
	// Facility
	Facility SyslogFacility
	// Include hostname in messages
	IncludeHostname bool
}

// SyslogWriter writes connection events to syslog
type SyslogWriter struct {
	config   SyslogConfig
	hostname string
	writer   *syslog.Writer
}

// NewSyslogWriter creates a new syslog writer
func NewSyslogWriter(cfg SyslogConfig) (*SyslogWriter, error) {
	var writer *syslog.Writer
	var err error

	// Get hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	// Create syslog writer
	if cfg.Network != "" && cfg.Address != "" {
		// Remote syslog
		priority := syslog.Priority(cfg.Facility) | syslog.LOG_INFO
		writer, err = syslog.Dial(cfg.Network, cfg.Address, priority, cfg.Tag)
	} else {
		// Local syslog
		priority := syslog.Priority(cfg.Facility) | syslog.LOG_INFO
		writer, err = syslog.New(priority, cfg.Tag)
	}

	if err != nil {
		return nil, fmt.Errorf("creating syslog writer: %w", err)
	}

	return &SyslogWriter{
		config:   cfg,
		hostname: hostname,
		writer:   writer,
	}, nil
}

// Close closes the syslog writer
func (w *SyslogWriter) Close() error {
	if w.writer != nil {
		return w.writer.Close()
	}
	return nil
}

// WriteConnection writes a connection event to syslog
func (w *SyslogWriter) WriteConnection(conn *Connection, event ConnectionEvent) error {
	msg := w.formatMessage(conn, event)

	switch event {
	case EventFailed, EventRejected:
		return w.writer.Warning(msg)
	case EventClosed:
		return w.writer.Info(msg)
	default:
		return w.writer.Info(msg)
	}
}

// WriteEstablished writes an established connection event
func (w *SyslogWriter) WriteEstablished(conn *Connection) error {
	msg := w.formatMessage(conn, EventEstablished)
	return w.writer.Info(msg)
}

// WriteFailed writes a failed connection event
func (w *SyslogWriter) WriteFailed(conn *Connection, reason string) error {
	msg := w.formatMessage(conn, EventFailed) + fmt.Sprintf(" reason=%s", reason)
	return w.writer.Warning(msg)
}

// WriteRejected writes a rejected connection event
func (w *SyslogWriter) WriteRejected(conn *Connection, reason string) error {
	msg := w.formatMessage(conn, EventRejected) + fmt.Sprintf(" reason=%s", reason)
	return w.writer.Warning(msg)
}

// formatMessage formats connection event as structured syslog message (RFC 5424 style)
func (w *SyslogWriter) formatMessage(conn *Connection, event ConnectionEvent) string {
	// RFC 5424 structured data format
	// CONN_<EVENT> src=<ip>:<port> dst=<ip>:<port> proto=<proto> dir=<dir> state=<state> [metadata]

	var eventStr string
	switch event {
	case EventNew:
		if conn.IsOutgoing() {
			eventStr = "CONN_OUT_NEW"
		} else {
			eventStr = "CONN_IN_NEW"
		}
	case EventEstablished:
		if conn.IsOutgoing() {
			eventStr = "CONN_OUT_ESTABLISHED"
		} else {
			eventStr = "CONN_IN_ACCEPTED"
		}
	case EventClosed:
		eventStr = "CONN_CLOSED"
	case EventFailed:
		eventStr = "CONN_OUT_FAILED"
	case EventRejected:
		eventStr = "CONN_IN_REJECTED"
	default:
		eventStr = "CONN_UNKNOWN"
	}

	// Build message parts
	parts := []string{
		eventStr,
		fmt.Sprintf("src=%s:%d", conn.SourceIP.String(), conn.SourcePort),
		fmt.Sprintf("dst=%s:%d", conn.DestIP.String(), conn.DestPort),
		fmt.Sprintf("proto=%s", w.protocolString(conn.Protocol)),
		fmt.Sprintf("dir=%s", conn.Direction.String()),
		fmt.Sprintf("state=%s", conn.State.String()),
	}

	// Add optional fields
	if conn.PID > 0 {
		parts = append(parts, fmt.Sprintf("pid=%d", conn.PID))
	}
	if conn.ProcessName != "" && conn.ProcessName != "unknown" {
		parts = append(parts, fmt.Sprintf("comm=%q", conn.ProcessName))
	}

	// Add timing information
	if event == EventEstablished && !conn.SynSentTime.IsZero() && !conn.EstablishedTime.IsZero() {
		rtt := conn.EstablishedTime.Sub(conn.SynSentTime)
		parts = append(parts, fmt.Sprintf("handshake_ms=%d", rtt.Milliseconds()))
	}

	if event == EventClosed && !conn.Timestamp.IsZero() {
		duration := conn.ClosedTime.Sub(conn.Timestamp)
		parts = append(parts, fmt.Sprintf("duration_s=%.1f", duration.Seconds()))
	}

	// Add hostname if configured
	if w.config.IncludeHostname {
		parts = append(parts, fmt.Sprintf("host=%s", w.hostname))
	}

	// Add timestamp
	parts = append(parts, fmt.Sprintf("ts=%s", conn.Timestamp.Format(time.RFC3339)))

	return joinStrings(parts, " ")
}

// protocolString returns protocol name
func (w *SyslogWriter) protocolString(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", proto)
	}
}

// joinStrings joins strings with separator
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}

	result := parts[0]
	for _, s := range parts[1:] {
		result += sep + s
	}
	return result
}

// TestSyslogWriter is a mock syslog writer for testing
type TestSyslogWriter struct {
	Messages []string
}

// NewTestSyslogWriter creates a test syslog writer
func NewTestSyslogWriter() *TestSyslogWriter {
	return &TestSyslogWriter{
		Messages: make([]string, 0),
	}
}

// Write writes a message (mock implementation)
func (w *TestSyslogWriter) Write(conn *Connection, event ConnectionEvent) error {
	// Create a simple writer for testing
	cfg := SyslogConfig{
		Tag:             "conntrack-test",
		Facility:        LOG_LOCAL0,
		IncludeHostname: false,
	}

	writer, err := NewSyslogWriter(cfg)
	if err != nil {
		// If syslog is not available, just store the message
		msg := fmt.Sprintf("%s src=%s:%d dst=%s:%d proto=%s dir=%s state=%s",
			event.String(),
			conn.SourceIP.String(), conn.SourcePort,
			conn.DestIP.String(), conn.DestPort,
			protocolString(conn.Protocol),
			conn.Direction.String(),
			conn.State.String(),
		)
		w.Messages = append(w.Messages, msg)
		return nil
	}
	defer writer.Close()

	return writer.WriteConnection(conn, event)
}

// protocolString converts protocol number to string
func protocolString(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", proto)
	}
}

// GetConnectionKey generates a unique key for a connection
func GetConnectionKey(sourceIP net.IP, sourcePort uint16, destIP net.IP, destPort uint16, protocol uint8) string {
	return makeConnectionKey(sourceIP, sourcePort, destIP, destPort, protocol)
}
