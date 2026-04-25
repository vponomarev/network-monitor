package discovery

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Hop represents a single hop in a network path
type Hop struct {
	TTL      int           `json:"ttl"`
	IP       net.IP        `json:"ip"`
	Hostname string        `json:"hostname,omitempty"`
	RTT      time.Duration `json:"rtt,omitempty"`
	Lost     bool          `json:"lost"`
	Device   string        `json:"device,omitempty"`
	Layer    string        `json:"layer,omitempty"`
}

// Path represents a complete network path between two hosts
type Path struct {
	SrcIP      net.IP        `json:"src_ip"`
	DstIP      net.IP        `json:"dst_ip"`
	Hops       []Hop         `json:"hops"`
	Discovered time.Time     `json:"discovered"`
	TTL        time.Duration `json:"ttl"` // Cache TTL
}

// PathID generates a unique identifier for a path
func (p *Path) PathID() string {
	return fmt.Sprintf("path-%s-%s", p.SrcIP.String(), p.DstIP.String())
}

// Bottleneck represents a network bottleneck
type Bottleneck struct {
	HopIP       string        `json:"hop_ip"`
	HopTTL      int           `json:"hop_ttl"`
	Device      string        `json:"device,omitempty"`
	LossPercent float64       `json:"loss_percent"`
	RTTAvg      time.Duration `json:"rtt_avg"`
}

// Tracerouter defines the interface for running traceroutes
type Tracerouter interface {
	// Run executes a traceroute from src to dst
	Run(ctx context.Context, src, dst string) (*Path, error)

	// RunWithTimeout executes a traceroute with custom timeout
	RunWithTimeout(ctx context.Context, src, dst string, timeout time.Duration) (*Path, error)
}

// DefaultTracerouter implements Tracerouter using system traceroute
type DefaultTracerouter struct {
	maxHops int
	timeout time.Duration
	probes  int
}

// NewDefaultTracerouter creates a new tracerouter with default settings
func NewDefaultTracerouter() *DefaultTracerouter {
	return &DefaultTracerouter{
		maxHops: 30,
		timeout: 2 * time.Second,
		probes:  3,
	}
}

// Run executes a traceroute
func (t *DefaultTracerouter) Run(ctx context.Context, src, dst string) (*Path, error) {
	return t.RunWithTimeout(ctx, src, dst, t.timeout)
}

// RunWithTimeout executes a traceroute with custom timeout
func (t *DefaultTracerouter) RunWithTimeout(ctx context.Context, src, dst string, timeout time.Duration) (*Path, error) {
	// Validate destination IP
	dstIP := net.ParseIP(dst)
	if dstIP == nil {
		return nil, fmt.Errorf("invalid destination IP: %s", dst)
	}

	srcIP := net.ParseIP(src)
	if srcIP == nil {
		// Use zero address if src not specified
		srcIP = net.IPv4zero
	}

	path := &Path{
		SrcIP:      srcIP,
		DstIP:      dstIP,
		Hops:       make([]Hop, 0),
		Discovered: time.Now(),
		TTL:        10 * time.Minute,
	}

	// In production, this would call system traceroute or use raw sockets
	// For now, return a placeholder path
	// Actual implementation will be in traceroute_linux.go

	return path, nil
}

// FindBottleneck analyzes a path and identifies the bottleneck hop
func FindBottleneck(path *Path) *Bottleneck {
	if path == nil || len(path.Hops) == 0 {
		return nil
	}

	var maxLoss float64
	var bottleneck *Bottleneck

	lostCount := 0
	for i, hop := range path.Hops {
		if hop.Lost {
			lostCount++
			// Calculate loss percentage up to this point
			lossPercent := float64(lostCount) / float64(i+1) * 100
			if lossPercent > maxLoss {
				maxLoss = lossPercent
				bottleneck = &Bottleneck{
					HopIP:       hop.IP.String(),
					HopTTL:      hop.TTL,
					Device:      hop.Device,
					LossPercent: lossPercent,
				}
			}
		}
	}

	return bottleneck
}

// TotalLoss returns the total packet loss percentage for a path
func (p *Path) TotalLoss() float64 {
	if len(p.Hops) == 0 {
		return 0
	}

	lostCount := 0
	for _, hop := range p.Hops {
		if hop.Lost {
			lostCount++
		}
	}

	return float64(lostCount) / float64(len(p.Hops)) * 100
}

// AvgRTT returns the average RTT for successful hops
func (p *Path) AvgRTT() time.Duration {
	var total time.Duration
	count := 0

	for _, hop := range p.Hops {
		if !hop.Lost && hop.RTT > 0 {
			total += hop.RTT
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / time.Duration(count)
}
