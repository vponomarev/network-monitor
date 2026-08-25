//go:build !linux

package irqaffinity

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type Options struct {
	Interval          time.Duration
	BusyThreshold     float64
	SysRoot, ProcRoot string
}
type Collector struct{}

func New(_ Options, _ *zap.Logger, _ prometheus.Registerer) *Collector { return &Collector{} }
func (*Collector) Run(ctx context.Context) error                       { <-ctx.Done(); return ctx.Err() }
