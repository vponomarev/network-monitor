//go:build linux

package conntrack

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func auditLiveWait(t *testing.T, label string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", label)
}

func auditLiveTracker(t *testing.T, port int, incoming bool, ttl time.Duration) (*Tracker, *prometheus.Registry) {
	t.Helper()
	if os.Getenv("NETMON_LIVE_TESTS") != "1" {
		t.Skip("set NETMON_LIVE_TESTS=1 on a root Linux test host")
	}
	reg := prometheus.NewRegistry()
	tracker, err := NewTracker(Config{TrackIncoming: incoming, TrackOutgoing: true, TrackCloses: true, FilterPorts: []int{port}, StateTTL: ttl, CleanupInterval: 100 * time.Millisecond, Registerer: reg}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tracker.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("tracker stop: %v", err)
			}
		case <-time.After(4 * time.Second):
			t.Error("tracker stop timeout")
		}
	})
	auditLiveWait(t, "tracker ready", tracker.Ready)
	return tracker, reg
}

func auditLiveListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func auditLiveConnect(t *testing.T, ln net.Listener, dialer *net.Dialer) (*net.TCPConn, net.Conn, time.Duration) {
	t.Helper()
	start := time.Now()
	conn, err := dialer.Dial("tcp4", ln.Addr().String())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ln.Accept()
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); server.Close() })
	return conn.(*net.TCPConn), server, elapsed
}

func TestAuditLiveLoopback(t *testing.T) {
	ln := auditLiveListener(t)
	tr, _ := auditLiveTracker(t, ln.Addr().(*net.TCPAddr).Port, true, time.Hour)
	auditLiveConnect(t, ln, &net.Dialer{Timeout: time.Second})
	time.Sleep(200 * time.Millisecond)
	stats := tr.GetStats()
	t.Logf("real eBPF loopback stats: %+v", stats)
	if stats.EstablishedOutgoing != 1 || stats.EstablishedIncoming != 1 {
		t.Fatalf("expected both socket sides; got %+v", stats)
	}
}

func TestAuditLiveTTL(t *testing.T) {
	ln := auditLiveListener(t)
	tr, _ := auditLiveTracker(t, ln.Addr().(*net.TCPAddr).Port, false, 800*time.Millisecond)
	client, server, _ := auditLiveConnect(t, ln, &net.Dialer{Timeout: time.Second})
	auditLiveWait(t, "established", func() bool { return tr.GetStats().Established == 1 })
	buf := make([]byte, 1)
	for i := 0; i < 15; i++ {
		client.SetDeadline(time.Now().Add(time.Second))
		server.SetDeadline(time.Now().Add(time.Second))
		if _, err := client.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		if _, err := server.Read(buf); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("TCP data still transfers after TTL; stats=%+v", tr.GetStats())
	if tr.GetStats().Established != 1 {
		t.Fatal("live established TCP connection removed by TTL")
	}
}

func TestAuditLiveTupleReuse(t *testing.T) {
	ln := auditLiveListener(t)
	tr, _ := auditLiveTracker(t, ln.Addr().(*net.TCPAddr).Port, false, time.Hour)
	client, server, _ := auditLiveConnect(t, ln, &net.Dialer{Timeout: time.Second})
	auditLiveWait(t, "first established", func() bool { return tr.GetStats().Established == 1 })
	local := client.LocalAddr().(*net.TCPAddr)
	client.SetLinger(0)
	client.Close()
	server.Close()
	auditLiveWait(t, "first closed", func() bool { cs := tr.GetConnections(); return len(cs) == 1 && cs[0].State == StateClosed })
	auditLiveConnect(t, ln, &net.Dialer{Timeout: time.Second, LocalAddr: local})
	auditLiveWait(t, "second established", func() bool { return tr.GetStats().Established == 1 })
	var c *Connection
	for _, candidate := range tr.GetConnections() {
		if candidate.State == StateEstablished {
			c = candidate
			break
		}
	}
	if c == nil {
		t.Fatal("missing new lifecycle")
		return
	}
	t.Logf("reused local port=%d state=%s ClosedTime=%v Duration=%v", local.Port, c.State, c.ClosedTime, c.Duration())
	if !c.ClosedTime.IsZero() {
		t.Fatal("reopened real socket retains previous close timestamp")
	}
}

func TestAuditLiveFailedConnect(t *testing.T) {
	ln := auditLiveListener(t)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	tr, reg := auditLiveTracker(t, port, false, time.Hour)
	c, err := net.DialTimeout("tcp4", ln.Addr().String(), time.Second)
	if err == nil {
		c.Close()
		t.Fatal("expected connection refused")
	}
	time.Sleep(250 * time.Millisecond)
	ms, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	failed := float64(0)
	for _, m := range ms {
		if m.GetName() != "conntrack_events_total" {
			continue
		}
		for _, v := range m.Metric {
			for _, l := range v.Label {
				if l.GetName() == "event" && l.GetValue() == "FAILED" {
					failed += v.GetCounter().GetValue()
				}
			}
		}
	}
	t.Logf("real refused connect: failed_count=%v state_entries=%d", failed, tr.GetConnectionCount())
	if failed != 1 {
		t.Fatal("refused TCP connect was not exported as FAILED")
	}
}

func TestAuditLiveHandshake(t *testing.T) {
	if os.Getenv("AUDIT_NETEM") != "1" {
		t.Skip("run inside audit-only netns with lo netem delay")
	}
	ln := auditLiveListener(t)
	tr, _ := auditLiveTracker(t, ln.Addr().(*net.TCPAddr).Port, false, time.Hour)
	_, _, elapsed := auditLiveConnect(t, ln, &net.Dialer{Timeout: 3 * time.Second})
	auditLiveWait(t, "established", func() bool { return tr.GetStats().Established == 1 })
	hs := tr.GetConnections()[0].HandshakeDuration()
	t.Logf("real connect=%v reported handshake=%v", elapsed, hs)
	if elapsed < 100*time.Millisecond {
		t.Fatal("netem fixture did not delay TCP handshake")
	}
	if hs < elapsed/2 {
		t.Fatal("reported handshake excludes actual network delay")
	}
}
