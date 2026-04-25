//go:build !linux
// +build !linux

package discovery

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// TracerouteConfig holds traceroute configuration
type TracerouteConfig struct {
	MaxHops      int           `json:"max_hops"`
	Timeout      time.Duration `json:"timeout"`
	ProbesPerHop int           `json:"probes_per_hop"`
	StartTTL     int           `json:"start_ttl"`
	Protocol     string        `json:"protocol"` // icmp, udp, tcp
	DstPort      int           `json:"dst_port"`
	SrcPort      int           `json:"src_port"`  // Source port for TCP traceroute
	TCPFlags     string        `json:"tcp_flags"` // TCP flags for probes
}

// HopResult represents a single hop result from traceroute
type HopResult struct {
	TTL       int           `json:"ttl"`
	IP        string        `json:"ip,omitempty"`
	Hostname  string        `json:"hostname,omitempty"`
	RTT       time.Duration `json:"rtt,omitempty"`
	Lost      bool          `json:"lost"`
	Timeout   bool          `json:"timeout"`
	ProbeSent time.Time     `json:"probe_sent"`
}

// TracerouteResult represents the result of a traceroute
type TracerouteResult struct {
	Destination string        `json:"destination"`
	Hops        []HopResult   `json:"hops"`
	Completed   bool          `json:"completed"`
	Duration    time.Duration `json:"duration"`
}

// DefaultTracerouteConfig returns default traceroute configuration
func DefaultTracerouteConfig() *TracerouteConfig {
	return &TracerouteConfig{
		MaxHops:      30,
		Timeout:      3 * time.Second,
		ProbesPerHop: 3,
		StartTTL:     1,
		Protocol:     "icmp",
		DstPort:      33434,
		SrcPort:      0,
		TCPFlags:     "S",
	}
}

// PacketTracerouter performs network traceroute using raw packets (Linux only)
type PacketTracerouter interface {
	Trace(ctx context.Context, dstIP string) (*TracerouteResult, error)
}

// TracerouteFactory creates tracerouters
type TracerouteFactory struct {
	config *TracerouteConfig
	logger *zap.Logger
}

// NewTracerouteFactory creates a new traceroute factory
func NewTracerouteFactory(config *TracerouteConfig, logger *zap.Logger) *TracerouteFactory {
	if config == nil {
		config = DefaultTracerouteConfig()
	}
	return &TracerouteFactory{
		config: config,
		logger: logger,
	}
}

// Create returns an error on non-Linux platforms
func (f *TracerouteFactory) Create(protocol string) (PacketTracerouter, error) {
	return nil, fmt.Errorf("traceroute not supported on this platform (requires Linux)")
}

// TraceroutePool manages concurrent traceroutes
type TraceroutePool struct {
	factory       *TracerouteFactory
	maxConcurrent int
	semaphore     chan struct{}
}

// NewTraceroutePool creates a new traceroute pool
func NewTraceroutePool(factory *TracerouteFactory, maxConcurrent int) *TraceroutePool {
	return &TraceroutePool{
		factory:       factory,
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
	}
}

// Trace returns error on non-Linux platforms
func (p *TraceroutePool) Trace(ctx context.Context, dstIP string) (*TracerouteResult, error) {
	return nil, fmt.Errorf("traceroute not supported on this platform (requires Linux)")
}

// TraceBatch returns error on non-Linux platforms
func (p *TraceroutePool) TraceBatch(ctx context.Context, dstIPs []string) ([]*TracerouteResult, error) {
	return nil, fmt.Errorf("traceroute not supported on this platform (requires Linux)")
}
