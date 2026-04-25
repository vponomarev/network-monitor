package collector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"go.uber.org/zap"
)

// TracePipePath is the default path to the kernel trace pipe
const TracePipePath = "/sys/kernel/tracing/trace_pipe"

// TCPRetransmitEvent represents a TCP retransmit event
type TCPRetransmitEvent struct {
	Timestamp time.Time
	SourceIP  string
	DestIP    string
}

// RetransmitExporter defines the interface for exporting retransmit events
type RetransmitExporter interface {
	RecordRetransmit(srcIP, dstIP string)
}

// TracePipeCollector reads from /sys/kernel/tracing/trace_pipe
type TracePipeCollector struct {
	path     string
	exporter RetransmitExporter
	logger   *zap.Logger
	pattern  *regexp.Regexp
}

// NewTracePipeCollector creates a new trace pipe collector
func NewTracePipeCollector(path string, exporter RetransmitExporter, logger *zap.Logger) *TracePipeCollector {
	// Pattern to match tcp_retransmit_skb events
	// New format: tcp_retransmit_skb: family=AF_INET sport=7005 dport=30792 saddr=10.181.208.50 daddr=10.179.64.23 ...
	// Old format: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.1 daddr=192.168.1.2 ...
	pattern := regexp.MustCompile(`tcp_retransmit_skb:.*?saddr=([0-9.]+).*?daddr=([0-9.]+)`)

	return &TracePipeCollector{
		path:     path,
		exporter: exporter,
		logger:   logger.Named("collector"),
		pattern:  pattern,
	}
}

// Run starts the collector
func (c *TracePipeCollector) Run(ctx context.Context) error {
	c.logger.Info("Starting trace pipe collector", zap.String("path", c.path))

	// Check if trace_pipe exists
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		return fmt.Errorf("trace_pipe not found at %s - ensure tracefs is mounted (sudo mount -t tracefs none /sys/kernel/tracing)", c.path)
	}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Stopping trace pipe collector")
			return ctx.Err()
		default:
		}

		if err := c.readTracePipe(ctx); err != nil {
			c.logger.Error("Error reading trace pipe", zap.Error(err))
			// Wait before retrying
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// readTracePipe reads from the trace pipe until error or context cancellation
func (c *TracePipeCollector) readTracePipe(ctx context.Context) error {
	file, err := os.Open(c.path)
	if err != nil {
		return fmt.Errorf("opening trace_pipe: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return fmt.Errorf("reading trace_pipe: %w", err)
		}

		c.processLine(line)
	}
}

// processLine processes a single line from trace pipe
func (c *TracePipeCollector) processLine(line string) {
	// Only process tcp_retransmit_skb events
	if !contains(line, "tcp_retransmit_skb") {
		return
	}

	matches := c.pattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		c.logger.Debug("No match in line", zap.String("line", line))
		return
	}

	srcIP := matches[1]
	dstIP := matches[2]

	c.logger.Debug("Retransmit detected",
		zap.String("src", srcIP),
		zap.String("dst", dstIP))

	c.exporter.RecordRetransmit(srcIP, dstIP)
}

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
