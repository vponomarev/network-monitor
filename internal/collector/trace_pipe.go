package collector

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// TracePipePath is the default path to the kernel trace pipe
const TracePipePath = "/sys/kernel/tracing/trace_pipe"

// TracepointEnablePath is the path to the tcp_retransmit_skb enable flag
const TracepointEnablePath = "/sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable"

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
	metrics  *CollectorMetrics

	// onReady, if set, is invoked exactly once after the trace_pipe is first
	// opened successfully — i.e. when the collector starts consuming events.
	onReady   func()
	readyOnce sync.Once
}

// SetReadyFunc registers a callback invoked once when the collector has
// successfully opened trace_pipe and begins consuming events. Used to signal
// readiness (see internal/health). Safe to call before Run.
func (c *TracePipeCollector) SetReadyFunc(f func()) {
	c.onReady = func() {
		if c.metrics != nil {
			c.metrics.SetUp(true)
		}
		if f != nil {
			f()
		}
	}
}

// signalReady invokes the onReady callback at most once.
func (c *TracePipeCollector) signalReady() {
	if c.onReady == nil {
		return
	}
	c.readyOnce.Do(c.onReady)
}

// NewTracePipeCollector creates a new trace pipe collector
func NewTracePipeCollector(path string, exporter RetransmitExporter, logger *zap.Logger, metrics *CollectorMetrics) *TracePipeCollector {
	// Pattern to match tcp_retransmit_skb events
	// New format: tcp_retransmit_skb: family=AF_INET sport=7005 dport=30792 saddr=10.181.208.50 daddr=10.179.64.23 ...
	// Old format: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.1 daddr=192.168.1.2 ...
	pattern := regexp.MustCompile(`tcp_retransmit_skb:.*?saddr=([0-9.]+).*?daddr=([0-9.]+)`)

	c := &TracePipeCollector{
		path:     path,
		exporter: exporter,
		logger:   logger.Named("collector"),
		pattern:  pattern,
		metrics:  metrics,
	}

	return c
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
			if errors.Is(err, context.Canceled) || errors.Is(err, unix.ECANCELED) {
				c.logger.Debug("Trace pipe collector stopped")
				return nil
			}
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

	// trace_pipe opened successfully — the collector is now consuming events.
	c.signalReady()

	reader := bufio.NewReader(file)

	// Create a channel for context cancellation
	done := ctx.Done()

	for {
		// Check context before blocking read
		select {
		case <-done:
			c.logger.Debug("Context cancelled, closing trace_pipe")
			file.Close()
			return ctx.Err()
		default:
		}

		// Use a goroutine to make the read cancellable
		type readResult struct {
			line string
			err  error
		}
		resultCh := make(chan readResult, 1)

		go func() {
			line, err := reader.ReadString('\n')
			resultCh <- readResult{line: line, err: err}
		}()

		// Wait for either read completion or context cancellation
		select {
		case <-done:
			c.logger.Debug("Context cancelled during read, closing file")
			file.Close()
			return ctx.Err()
		case result := <-resultCh:
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				return fmt.Errorf("reading trace_pipe: %w", result.err)
			}
			c.processLine(result.line)
		}
	}
}

// processLine processes a single line from trace pipe
func (c *TracePipeCollector) processLine(line string) {
	// Count all lines read
	if c.metrics != nil {
		c.metrics.IncEventsRead()
	}

	// Only process tcp_retransmit_skb events
	if !contains(line, "tcp_retransmit_skb") {
		return
	}

	// tcp_retransmit_skb also reports repeated SYN/SYN-ACK packets. Those are
	// connection-establishment failures, not loss on an established flow.
	if isSYNHandshakeRetransmit(line) {
		return
	}

	matches := c.pattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		c.logger.Debug("No match in line", zap.String("line", line))
		if c.metrics != nil {
			c.metrics.IncParseErrors()
		}
		return
	}

	srcIP := matches[1]
	dstIP := matches[2]

	c.logger.Debug("Retransmit detected",
		zap.String("src", srcIP),
		zap.String("dst", dstIP))

	c.exporter.RecordRetransmit(srcIP, dstIP)

	if c.metrics != nil {
		c.metrics.IncEventsParsed()
	}
}

func isSYNHandshakeRetransmit(line string) bool {
	return strings.Contains(line, "state=TCP_SYN_SENT") ||
		strings.Contains(line, "state=TCP_SYN_RECV") ||
		strings.Contains(line, "state=TCP_NEW_SYN_RECV")
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

// IsTracepointEnabled checks if the tcp_retransmit_skb tracepoint is enabled
func IsTracepointEnabled(enablePath string) (bool, error) {
	data, err := os.ReadFile(enablePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("tracepoint enable file not found at %s - ensure tracefs is mounted", enablePath)
		}
		return false, fmt.Errorf("reading enable file: %w", err)
	}

	value := strings.TrimSpace(string(data))
	return value == "1", nil
}

// EnableTracepoint enables the tcp_retransmit_skb tracepoint
func EnableTracepoint(enablePath string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(enablePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("tracepoint directory not found at %s - ensure tracefs is mounted", dir)
	}

	// Write "1" to enable the tracepoint
	if err := os.WriteFile(enablePath, []byte("1"), 0644); err != nil {
		return fmt.Errorf("enabling tracepoint: %w (requires root privileges)", err)
	}

	return nil
}

// CheckAndWarnTracepoint checks if tracepoint is enabled and logs a warning if not
func CheckAndWarnTracepoint(logger *zap.Logger, enablePath string) bool {
	enabled, err := IsTracepointEnabled(enablePath)
	if err != nil {
		logger.Warn("Cannot check tracepoint status",
			zap.String("path", enablePath),
			zap.Error(err))
		return false
	}

	if !enabled {
		logger.Warn("TCP retransmit tracepoint is NOT enabled. "+
			"Run with --enable-tracing flag or manually enable: "+
			"echo 1 | sudo tee "+enablePath,
			zap.String("path", enablePath))
		return false
	}

	logger.Info("TCP retransmit tracepoint is enabled", zap.String("path", enablePath))
	return true
}

// GetTracepointEnablePath returns the path to the enable file for tcp_retransmit_skb
func GetTracepointEnablePath() string {
	return TracepointEnablePath
}
