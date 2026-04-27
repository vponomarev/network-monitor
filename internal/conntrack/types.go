package conntrack

import (
	"net"
	"time"
)

// DefaultEBPFProgramPath is the default path to the eBPF program object file
const DefaultEBPFProgramPath = "/usr/share/netmon/bpf/conntrack.bpf.o"

// Config holds connection tracker configuration
type Config struct {
	// Path to eBPF program object file
	EBPFProgramPath string

	// Track incoming connections
	TrackIncoming bool

	// Track outgoing connections
	TrackOutgoing bool

	// Track connection closes
	TrackCloses bool

	// Filter by ports (empty = all ports)
	FilterPorts []int

	// Syslog configuration
	Syslog SyslogConfig

	// SYN timeout
	SYNTimeout time.Duration
}

// Direction represents connection direction
type Direction int

const (
	DirectionIncoming Direction = iota
	DirectionOutgoing
)

func (d Direction) String() string {
	switch d {
	case DirectionIncoming:
		return "incoming"
	case DirectionOutgoing:
		return "outgoing"
	default:
		return "unknown"
	}
}

// IPFromBytes converts 16-byte array to net.IP (handles IPv4-mapped IPv6)
func IPFromBytes(b [16]byte) net.IP {
	// Check if IPv4-mapped IPv6
	if b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 0 &&
		b[4] == 0 && b[5] == 0 && b[6] == 0 && b[7] == 0 &&
		b[8] == 0 && b[9] == 0 && b[10] == 0xff && b[11] == 0xff {
		// Return IPv4
		return net.IPv4(b[12], b[13], b[14], b[15])
	}
	// Return IPv6
	return net.IP(b[:])
}

// Connection represents a tracked network connection
type Connection struct {
	// Unique identifier
	ID string

	// Network information
	SourceIP   net.IP
	SourcePort uint16
	DestIP     net.IP
	DestPort   uint16
	Protocol   uint8

	// Connection metadata
	Direction   Direction
	State       ConnectionState
	Timestamp   time.Time
	LastUpdated time.Time

	// Process information
	PID         uint32
	ProcessName string

	// TCP handshake tracking
	SynAckReceived bool
	Accepted       bool
	Established    bool

	// Timing
	SynSentTime     time.Time
	SynAckTime      time.Time
	EstablishedTime time.Time
	ClosedTime      time.Time

	// Statistics
	BytesSent uint64
	BytesRecv uint64
}

// Duration returns the duration of the connection
func (c *Connection) Duration() time.Duration {
	if c.ClosedTime.IsZero() {
		return time.Since(c.Timestamp)
	}
	return c.ClosedTime.Sub(c.Timestamp)
}

// HandshakeDuration returns the TCP handshake duration (SYN to ESTABLISHED)
func (c *Connection) HandshakeDuration() time.Duration {
	if c.EstablishedTime.IsZero() {
		return 0
	}
	if c.SynSentTime.IsZero() {
		return 0
	}
	return c.EstablishedTime.Sub(c.SynSentTime)
}

// IsOutgoing returns true if this is an outgoing connection
func (c *Connection) IsOutgoing() bool {
	return c.Direction == DirectionOutgoing
}

// IsIncoming returns true if this is an incoming connection
func (c *Connection) IsIncoming() bool {
	return c.Direction == DirectionIncoming
}
