package main

import (
	"github.com/vponomarev/network-monitor/internal/discovery"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
)

// retransmitRecorder fans a raw event out to the bounded unknown-metadata
// inventory before the production exporter aggregates away IP labels.
type retransmitRecorder struct {
	exporter  *metrics.Exporter
	unknown   *metadata.UnknownTracker
	discovery *discovery.DiscoveryService
}

func (r *retransmitRecorder) RecordRetransmit(srcIP, dstIP string) {
	if r.unknown != nil {
		src, dst := r.exporter.RecordRetransmitResolved(srcIP, dstIP)
		r.unknown.ObservePairResolved(srcIP, dstIP, src, dst)
	} else {
		r.exporter.RecordRetransmit(srcIP, dstIP)
	}
	if r.discovery != nil {
		r.discovery.RecordLoss(srcIP, dstIP)
	}
}
