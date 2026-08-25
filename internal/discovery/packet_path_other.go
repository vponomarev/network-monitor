//go:build !linux

package discovery

import (
	"fmt"

	"go.uber.org/zap"
)

func NewPacketPathTracerouter(_ *TracerouteConfig, _ *zap.Logger, _ int) (Tracerouter, error) {
	return nil, fmt.Errorf("packet traceroute is only supported on Linux")
}
