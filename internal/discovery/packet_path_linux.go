//go:build linux

package discovery

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

// NewPacketPathTracerouter adapts the Linux packet traceroute implementation
// to the path-discovery interface used by the production service.
func NewPacketPathTracerouter(config *TracerouteConfig, logger *zap.Logger, maxConcurrent int) (Tracerouter, error) {
	if config == nil {
		config = DefaultTracerouteConfig()
	}
	if config.Protocol != "" && config.Protocol != "icmp" {
		return nil, fmt.Errorf("traceroute protocol %q is not production-ready; use icmp", config.Protocol)
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &packetPathTracerouter{
		pool: NewTraceroutePool(NewTracerouteFactory(config, logger), maxConcurrent),
		ttl:  10 * time.Minute,
	}, nil
}

type packetPathTracerouter struct {
	pool *TraceroutePool
	ttl  time.Duration
}

func (t *packetPathTracerouter) Run(ctx context.Context, src, dst string) (*Path, error) {
	return t.RunWithTimeout(ctx, src, dst, 0)
}

func (t *packetPathTracerouter) RunWithTimeout(ctx context.Context, src, dst string, timeout time.Duration) (*Path, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result, err := t.pool.TraceFrom(ctx, src, dst)
	if err != nil {
		return nil, err
	}
	srcIP := net.ParseIP(src)
	if srcIP == nil {
		return nil, fmt.Errorf("invalid source IP: %s", src)
	}
	dstIP := net.ParseIP(dst)
	if dstIP == nil {
		return nil, fmt.Errorf("invalid destination IP: %s", dst)
	}

	path := &Path{
		SrcIP: srcIP, DstIP: dstIP, Discovered: time.Now(), TTL: t.ttl,
		Hops: make([]Hop, 0, len(result.Hops)),
	}
	for _, hop := range result.Hops {
		path.Hops = append(path.Hops, Hop{
			TTL: hop.TTL, IP: net.ParseIP(hop.IP), Hostname: hop.Hostname,
			RTT: hop.RTT, Lost: hop.Lost, ProbesSent: hop.ProbesSent,
			ProbesReceived: hop.ProbesReceived, LossPercent: hop.LossPercent,
		})
	}
	return path, nil
}
