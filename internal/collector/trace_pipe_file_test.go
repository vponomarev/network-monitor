package collector

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockExporterForFileTest is a simple mock for testing
type mockExporterForFileTest struct {
	events []TCPRetransmitEvent
}

func (m *mockExporterForFileTest) RecordRetransmit(srcIP, dstIP string) {
	m.events = append(m.events, TCPRetransmitEvent{
		SourceIP: srcIP,
		DestIP:   dstIP,
	})
}

func TestTracePipeCollector_WithRealData(t *testing.T) {
	// Check if test data file exists (relative to project root)
	testDataFile := "../../testdata/trace_pipe_sample.txt"
	if _, err := os.Stat(testDataFile); os.IsNotExist(err) {
		t.Skip("Test data file not found - run scripts/collect_trace_data.sh first")
	}

	// Open the file
	file, err := os.Open(testDataFile)
	require.NoError(t, err)
	defer file.Close()

	// Create collector with mock exporter
	logger := zap.NewNop()
	exporter := &mockExporterForFileTest{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(testDataFile, exporter, logger)

	// Read and process first 100 lines
	scanner := bufio.NewScanner(file)
	lineCount := 0
	maxLines := 100

	for scanner.Scan() && lineCount < maxLines {
		line := scanner.Text()
		collector.processLine(line)
		lineCount++
	}

	require.NoError(t, scanner.Err())

	// Verify we captured some events
	t.Logf("Processed %d lines, captured %d retransmit events", lineCount, len(exporter.events))
	assert.Greater(t, len(exporter.events), 0, "Should capture at least one retransmit event")

	// Verify IP format
	if len(exporter.events) > 0 {
		event := exporter.events[0]
		t.Logf("First event: %s -> %s", event.SourceIP, event.DestIP)
		assert.NotEmpty(t, event.SourceIP)
		assert.NotEmpty(t, event.DestIP)
		assert.Contains(t, event.SourceIP, ".")
		assert.Contains(t, event.DestIP, ".")
	}
}

func TestTracePipeCollector_ParseAllFormats(t *testing.T) {
	// Test various formats found in real data
	testCases := []struct {
		line      string
		wantSrc   string
		wantDst   string
		wantMatch bool
	}{
		{
			line:      "          <idle>-0       [077] ..s.. 20660829.667623: tcp_retransmit_skb: family=AF_INET sport=7005 dport=30792 saddr=10.181.208.50 daddr=10.179.64.23 saddrv6=::ffff:10.181.208.50 daddrv6=::ffff:10.179.64.23 state=TCP_ESTABLISHED",
			wantSrc:   "10.181.208.50",
			wantDst:   "10.179.64.23",
			wantMatch: true,
		},
		{
			line:      "         radosgw-2855037 [034] ..s.. 20660830.025212: tcp_retransmit_skb: family=AF_INET sport=83 dport=11746 saddr=10.181.208.50 daddr=10.181.208.80 saddrv6=::ffff:10.181.208.50 daddrv6=::ffff:10.181.208.80 state=TCP_ESTABLISHED",
			wantSrc:   "10.181.208.50",
			wantDst:   "10.181.208.80",
			wantMatch: true,
		},
		{
			line:      "          <...>-12345 [001] d.H. 12345.678901: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.10 daddr=192.168.1.20 seq=123456789",
			wantSrc:   "192.168.1.10",
			wantDst:   "192.168.1.20",
			wantMatch: true,
		},
		{
			line:      "          <...>-12346 [002] d.H. 12346.789012: tcp_connect: saddr=192.168.1.10 daddr=192.168.1.20",
			wantMatch: false, // Not a retransmit event
		},
		{
			line:      "random garbage line",
			wantMatch: false,
		},
	}

	for _, tc := range testCases {
		logger := zap.NewNop()
		exporter := &mockExporterForFileTest{events: make([]TCPRetransmitEvent, 0)}
		collector := NewTracePipeCollector("/dev/null", exporter, logger)

		collector.processLine(tc.line)

		if tc.wantMatch {
			assert.Equal(t, 1, len(exporter.events), "Should match: %s", tc.line[:min(50, len(tc.line))])
			if len(exporter.events) > 0 {
				assert.Equal(t, tc.wantSrc, exporter.events[0].SourceIP)
				assert.Equal(t, tc.wantDst, exporter.events[0].DestIP)
			}
		} else {
			assert.Equal(t, 0, len(exporter.events), "Should not match: %s", tc.line[:min(50, len(tc.line))])
		}
	}
}

func TestTracePipeCollector_StatisticsFromRealData(t *testing.T) {
	// Check if test data file exists (relative to project root)
	testDataFile := "../../testdata/trace_pipe_sample.txt"
	if _, err := os.Stat(testDataFile); os.IsNotExist(err) {
		t.Skip("Test data file not found")
	}

	// Open the file
	file, err := os.Open(testDataFile)
	require.NoError(t, err)
	defer file.Close()

	// Create collector with mock exporter
	logger := zap.NewNop()
	exporter := &mockExporterForFileTest{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(testDataFile, exporter, logger)

	// Process entire file
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		collector.processLine(line)
		lineCount++
	}

	require.NoError(t, scanner.Err())

	// Statistics
	t.Logf("Total lines: %d", lineCount)
	t.Logf("Retransmit events: %d", len(exporter.events))
	t.Logf("Capture rate: %.2f%%", float64(len(exporter.events))/float64(lineCount)*100)

	// Count unique IP pairs
	uniquePairs := make(map[string]bool)
	for _, event := range exporter.events {
		key := event.SourceIP + "->" + event.DestIP
		uniquePairs[key] = true
	}

	t.Logf("Unique IP pairs: %d", len(uniquePairs))

	// Show top 5 most frequent pairs
	pairCount := make(map[string]int)
	for _, event := range exporter.events {
		key := event.SourceIP + "->" + event.DestIP
		pairCount[key]++
	}

	// This is just for debugging - actual analysis would be done in production
	if len(pairCount) > 0 {
		t.Log("Top 5 IP pairs by retransmit count:")
		for pair, count := range pairCount {
			if count >= 10 { // Only show pairs with 10+ retransmits
				t.Logf("  %s: %d", pair, count)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
