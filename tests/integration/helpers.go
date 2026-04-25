// Package integration contains helper functions for integration tests
package integration

import (
	"fmt"
	"net"
	"time"
)

// GenerateTraffic generates network traffic on the specified interface
func GenerateTraffic(target string, count int, duration time.Duration) error {
	for i := 0; i < count; i++ {
		conn, err := net.DialTimeout("tcp", target, duration)
		if err != nil {
			continue
		}
		// Send some data
		_, _ = conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
		conn.Close()
	}
	return nil
}

// GenerateUDPTraffic generates UDP traffic
func GenerateUDPTraffic(target string, count int) error {
	conn, err := net.Dial("udp", target)
	if err != nil {
		return err
	}
	defer conn.Close()

	for i := 0; i < count; i++ {
		_, _ = conn.Write([]byte(fmt.Sprintf("packet %d", i)))
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// WaitForPort waits for a port to become available
func WaitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("port %d not available within %v", port, timeout)
}

// GetLocalIP returns a local IP address
func GetLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
