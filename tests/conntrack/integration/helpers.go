// Package integration содержит вспомогательные функции для интеграционных тестов conntrack
package integration

import (
	"fmt"
	"net"
	"time"
)

// GenerateTraffic генерирует сетевой трафик на указанный адрес
func GenerateTraffic(target string, count int, duration time.Duration) error {
	for i := 0; i < count; i++ {
		conn, err := net.DialTimeout("tcp", target, duration)
		if err != nil {
			continue
		}
		_, _ = conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
		conn.Close()
	}
	return nil
}

// GenerateUDPTraffic генерирует UDP трафик
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

// WaitForPort ожидает доступности порта
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

// GetLocalIP возвращает локальный IP адрес
func GetLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// StartTestServer запускает тестовый TCP сервер для приёмки соединений
func StartTestServer(port int) (net.Listener, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
			}(conn)
		}
	}()

	return listener, nil
}
