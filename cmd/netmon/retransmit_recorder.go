package main

import (
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
)

// retransmitRecorder fans a raw event out to the bounded unknown-metadata
// inventory before the production exporter aggregates away IP labels.
type retransmitRecorder struct {
	exporter *metrics.Exporter
	unknown  *metadata.UnknownTracker
}

func (r *retransmitRecorder) RecordRetransmit(srcIP, dstIP string) {
	if r.unknown != nil {
		r.unknown.ObservePair(srcIP, dstIP)
	}
	r.exporter.RecordRetransmit(srcIP, dstIP)
}
