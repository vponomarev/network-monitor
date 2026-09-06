//go:build linux

package conntrack

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAuditLiveNamespaceIsolation(t *testing.T) {
	if os.Getenv("NETMON_LIVE_TESTS") != "1" {
		t.Skip("requires root Linux and iproute2")
	}
	listener := auditLiveListener(t)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	tracker, _ := auditLiveTracker(t, port, false, time.Hour)
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("netmon-identity-%d-%d", os.Getpid(), i)
		if out, err := exec.Command("ip", "netns", "add", name).CombinedOutput(); err != nil {
			t.Fatalf("create namespace: %s: %v", out, err)
		}
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", name).Run() })
		if out, err := exec.Command("ip", "-n", name, "link", "set", "lo", "up").CombinedOutput(); err != nil {
			t.Fatalf("bring up loopback: %s: %v", out, err)
		}
		program := `import socket,sys
s=socket.socket();s.bind(('127.0.0.1',int(sys.argv[1])));s.listen()
c=socket.socket();c.bind(('127.0.0.2',32001));c.connect(s.getsockname())
a,_=s.accept();print('ready',flush=True);sys.stdin.read(1)
`
		worker := exec.Command("ip", "netns", "exec", name, "python3", "-u", "-c", program, fmt.Sprint(port))
		stdin, err := worker.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := worker.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = worker.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stdin.Close(); _ = worker.Process.Kill(); _ = worker.Wait() })
		ready := make(chan string, 1)
		go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
		select {
		case line := <-ready:
			if line != "ready\n" {
				t.Fatalf("worker failed: %q", line)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("namespace worker timeout")
		}
	}
	auditLiveWait(t, "two identical tuples in different namespaces", func() bool { return tracker.GetStats().EstablishedOutgoing == 2 })
	connections := tracker.GetConnections()
	if len(connections) != 2 || connections[0].ID == connections[1].ID || connections[0].PID == connections[1].PID {
		t.Fatalf("socket/process identities collapsed: %+v", connections)
	}
	t.Log("same TCP tuple in two network namespaces retains two socket and process identities")
}
