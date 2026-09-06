package conntrack

import (
	"fmt"
	"log/syslog"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// SyslogFacility represents syslog facility
type SyslogFacility int

const (
	// LogUser is the user-level syslog facility.
	LogUser SyslogFacility = 8
	// LogDaemon is the system-daemon syslog facility.
	LogDaemon SyslogFacility = 24
	// LogLocal0 through LogLocal7 are locally defined syslog facilities.
	LogLocal0 SyslogFacility = 128
	LogLocal1 SyslogFacility = 136
	LogLocal2 SyslogFacility = 144
	LogLocal3 SyslogFacility = 152
	LogLocal4 SyslogFacility = 160
	LogLocal5 SyslogFacility = 168
	LogLocal6 SyslogFacility = 176
	LogLocal7 SyslogFacility = 184
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
	remote   net.Conn
	mu       sync.Mutex
}

// NewSyslogWriter creates a new syslog writer
func NewSyslogWriter(cfg SyslogConfig) (*SyslogWriter, error) {
	var writer *syslog.Writer
	var remote net.Conn
	var err error

	// Get hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	// Create syslog writer
	if cfg.Network != "" && cfg.Address != "" {
		// Remote syslog
		remote, err = net.DialTimeout(cfg.Network, cfg.Address, 2*time.Second)
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
		remote:   remote,
	}, nil
}

// Close closes the syslog writer
func (w *SyslogWriter) Close() error {
	if w.remote != nil {
		return w.remote.Close()
	}
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
		return w.write(msg, 4)
	case EventClosed:
		return w.write(msg, 6)
	default:
		return w.write(msg, 6)
	}
}

// WriteEstablished writes an established connection event
func (w *SyslogWriter) WriteEstablished(conn *Connection) error {
	msg := w.formatMessage(conn, EventEstablished)
	return w.write(msg, 6)
}

// WriteFailed writes a failed connection event
func (w *SyslogWriter) WriteFailed(conn *Connection, reason string) error {
	msg := w.formatMessage(conn, EventFailed) + fmt.Sprintf(" reason=%s", reason)
	return w.write(msg, 4)
}

// WriteRejected writes a rejected connection event
func (w *SyslogWriter) WriteRejected(conn *Connection, reason string) error {
	msg := w.formatMessage(conn, EventRejected) + fmt.Sprintf(" reason=%s", reason)
	return w.write(msg, 4)
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
	if event == EventEstablished && conn.HandshakeDuration() > 0 {
		rtt := conn.HandshakeDuration()
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

	return strings.Join(parts, " ")
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

// GetConnectionKey generates a unique key for a connection
func GetConnectionKey(sourceIP net.IP, sourcePort uint16, destIP net.IP, destPort uint16, protocol uint8) string {
	return makeConnectionKey(sourceIP, sourcePort, destIP, destPort, protocol)
}

func (w *SyslogWriter) write(msg string, severity int) error {
	if w.remote == nil {
		if severity == 4 {
			return w.writer.Warning(msg)
		}
		return w.writer.Info(msg)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.remote.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w.remote, "<%d>%s %s %s[%d]: %s\n", int(w.config.Facility)|severity, time.Now().Format(time.Stamp), w.hostname, w.config.Tag, os.Getpid(), strings.ReplaceAll(msg, "\n", " "))
	return err
}
