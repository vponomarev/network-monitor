package discovery

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Hop represents a single hop in a network path
type Hop struct {
	TTL            int           `json:"ttl"`
	IP             net.IP        `json:"ip"`
	Hostname       string        `json:"hostname,omitempty"`
	RTT            time.Duration `json:"rtt,omitempty"`
	Lost           bool          `json:"lost"`
	Device         string        `json:"device,omitempty"`
	Layer          string        `json:"layer,omitempty"`
	ProbesSent     int           `json:"probes_sent,omitempty"`
	ProbesReceived int           `json:"probes_received,omitempty"`
	LossPercent    float64       `json:"loss_percent"`
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

// FindBottleneck analyzes a path and identifies the bottleneck hop
func FindBottleneck(path *Path) *Bottleneck {
	if path == nil || len(path.Hops) == 0 {
		return nil
	}

	var maxLoss float64
	var bottleneck *Bottleneck
	for _, hop := range path.Hops {
		lossPercent := hop.LossPercent
		if hop.ProbesSent == 0 && hop.Lost {
			lossPercent = 100
		}
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

	return bottleneck
}

// TotalLoss returns the total packet loss percentage for a path
func (p *Path) TotalLoss() float64 {
	if len(p.Hops) == 0 {
		return 0
	}

	totalSent, totalReceived := 0, 0
	for _, hop := range p.Hops {
		if hop.ProbesSent > 0 {
			totalSent += hop.ProbesSent
			totalReceived += hop.ProbesReceived
		} else {
			totalSent++
			if !hop.Lost {
				totalReceived++
			}
		}
	}
	if totalSent == 0 {
		return 0
	}
	return float64(totalSent-totalReceived) / float64(totalSent) * 100
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
