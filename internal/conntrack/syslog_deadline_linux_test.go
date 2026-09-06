//go:build linux

package conntrack

import (
	"net"
	"testing"
	"time"
)

func TestRemoteSyslogCloseInterruptsBlockedWrite(t *testing.T) {
	writer, reader := net.Pipe()
	defer reader.Close()
	w := &SyslogWriter{remote: writer}
	done := make(chan error, 1)
	go func() { done <- w.WriteConnection(&Connection{}, EventEstablished) }()
	time.Sleep(20 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked write unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not interrupt write")
	}
}

func TestRemoteSyslogWriteDeadline(t *testing.T) {
	writer, reader := net.Pipe()
	defer writer.Close()
	defer reader.Close()
	w := &SyslogWriter{remote: writer}
	start := time.Now()
	if err := w.WriteConnection(&Connection{}, EventEstablished); err == nil {
		t.Fatal("expected deadline error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("unbounded write: %v", elapsed)
	}
}
