//go:build linux

package discovery

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestICMPReplyRequiresDestinationAndNonce(t *testing.T) {
	packet := createICMPEchoRequest(7)
	packet[0] = 0
	id := binary.BigEndian.Uint16(packet[4:6])
	dst := net.ParseIP("127.0.0.3")
	require.True(t, matchesICMPReply(packet, dst, dst, id, 7, packet[8:]))
	require.False(t, matchesICMPReply(packet, net.ParseIP("127.0.0.2"), dst, id, 7, packet[8:]))
	require.False(t, matchesICMPReply(packet, dst, dst, id, 7, []byte("previous probe")))
	quoted := make([]byte, 36)
	quoted[0], quoted[8], quoted[17] = 11, 0x45, 1
	copy(quoted[24:28], dst.To4())
	copy(quoted[28:], packet[:8])
	quoted[28] = 8
	require.True(t, matchesICMPReply(quoted, net.ParseIP("127.0.0.2"), dst, id, 7, nil))
	require.False(t, matchesICMPReply(quoted, dst, net.ParseIP("127.0.0.4"), id, 7, nil))
}

func TestAuditLiveICMPSourceAndCorrelation(t *testing.T) {
	if os.Getenv("NETMON_LIVE_TESTS") != "1" {
		t.Skip("requires root Linux raw sockets")
	}
	first, err := createICMPConnection()
	require.NoError(t, err)
	defer first.Close()
	second, err := createICMPConnection()
	require.NoError(t, err)
	defer second.Close()
	packet := createICMPEchoRequest(77)
	id := binary.BigEndian.Uint16(packet[4:6])
	require.NoError(t, first.SendTo(packet, net.ParseIP("127.0.0.2")))
	deadline := time.Now().Add(time.Second)
	for {
		require.NoError(t, second.SetReadDeadline(deadline))
		data, from, err := second.RecvFrom()
		require.NoError(t, err)
		if len(data) > 0 && data[0] == 0 {
			require.False(t, matchesICMPReply(data, from, net.ParseIP("127.0.0.3"), id, 77, packet[8:]))
			t.Log("real cross-destination ICMP reply rejected")
			break
		}
	}
	cfg := DefaultTracerouteConfig()
	cfg.MaxHops = 2
	cfg.ProbesPerHop = 1
	cfg.Timeout = time.Second
	tracer, err := NewPacketPathTracerouter(cfg, zap.NewNop(), 2)
	require.NoError(t, err)
	path, err := tracer.Run(context.Background(), "127.0.0.2", "127.0.0.3")
	require.NoError(t, err)
	require.True(t, path.Hops[len(path.Hops)-1].IP.Equal(net.ParseIP("127.0.0.3")))
	_, err = tracer.Run(context.Background(), "192.0.2.123", "127.0.0.3")
	require.Error(t, err, "non-local source must not silently use another address")
}

func TestIntermediateICMPLossDoesNotImplyPathLoss(t *testing.T) {
	dst := net.ParseIP("192.0.2.2")
	path := &Path{DstIP: dst, Hops: []Hop{
		{TTL: 1, Lost: true, ProbesSent: 3},
		{TTL: 2, IP: dst, ProbesSent: 3, ProbesReceived: 3},
	}}
	require.Nil(t, FindBottleneck(path))
	require.NotNil(t, path.DestinationProbeLoss())
	require.Equal(t, float64(0), *path.DestinationProbeLoss())
	path.Hops = path.Hops[:1]
	require.Nil(t, path.DestinationProbeLoss())
}
