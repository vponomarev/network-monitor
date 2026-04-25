package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLossTracker(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)
	require.NotNil(t, tracker)
	assert.Equal(t, 0, tracker.Count())
}

func TestDefaultLossTracker(t *testing.T) {
	tracker := DefaultLossTracker()
	require.NotNil(t, tracker)
	assert.Equal(t, 0, tracker.Count())
}

func TestLossTracker_RecordLoss(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	// First loss
	tracker.RecordLoss("192.168.1.1", "192.168.1.2")
	assert.Equal(t, 1, tracker.Count())

	pair, ok := tracker.GetPair("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, uint64(1), pair.LossCount)

	// Second loss
	tracker.RecordLoss("192.168.1.1", "192.168.1.2")
	pair, ok = tracker.GetPair("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, uint64(2), pair.LossCount)
}

func TestLossTracker_GetPair_NotFound(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	_, ok := tracker.GetPair("192.168.1.1", "192.168.1.2")
	assert.False(t, ok)
}

func TestLossTracker_GetTopPairs(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	// Add pairs with different loss counts
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")

	tracker.RecordLoss("3.3.3.3", "4.4.4.4")
	tracker.RecordLoss("3.3.3.3", "4.4.4.4")

	tracker.RecordLoss("5.5.5.5", "6.6.6.6")

	top := tracker.GetTopPairs(2)
	require.Len(t, top, 2)
	assert.Equal(t, uint64(3), top[0].LossCount) // Highest first
	assert.Equal(t, uint64(2), top[1].LossCount)
}

func TestLossTracker_GetTopPairsByRate(t *testing.T) {
	tracker := NewLossTracker(1 * time.Minute)

	// Add pairs
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("3.3.3.3", "4.4.4.4")

	top := tracker.GetTopPairsByRate(2)
	require.Len(t, top, 2)
	// First pair should have higher rate
	assert.Greater(t, top[0].LossRate, top[1].LossRate)
}

func TestLossTracker_GetAllPairs(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("3.3.3.3", "4.4.4.4")

	all := tracker.GetAllPairs()
	assert.Len(t, all, 2)
}

func TestLossTracker_Clear(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	tracker.RecordLoss("3.3.3.3", "4.4.4.4")
	assert.Equal(t, 2, tracker.Count())

	tracker.Clear()
	assert.Equal(t, 0, tracker.Count())
}

func TestLossTracker_Cleanup(t *testing.T) {
	tracker := NewLossTracker(10 * time.Millisecond)

	// Add a pair
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")
	assert.Equal(t, 1, tracker.Count())

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	removed := tracker.Cleanup()
	assert.Equal(t, 1, removed)
	assert.Equal(t, 0, tracker.Count())
}

func TestLossTracker_StartCleanup(t *testing.T) {
	tracker := NewLossTracker(10 * time.Millisecond)
	stopCh := make(chan struct{})

	tracker.StartCleanup(stopCh, 5*time.Millisecond)

	// Add a pair
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")

	// Wait for cleanup
	time.Sleep(30 * time.Millisecond)

	// Should be cleaned up
	assert.Equal(t, 0, tracker.Count())

	close(stopCh)
}

func TestLossTracker_GetPair_ReturnsCopy(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)
	tracker.RecordLoss("1.1.1.1", "2.2.2.2")

	pair1, ok := tracker.GetPair("1.1.1.1", "2.2.2.2")
	require.True(t, ok)

	// Modify the returned copy
	pair1.LossCount = 999

	// Original should be unchanged
	pair2, _ := tracker.GetPair("1.1.1.1", "2.2.2.2")
	assert.Equal(t, uint64(1), pair2.LossCount)
}

func TestLossTracker_MultiplePairs(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)

	// Add many pairs
	for i := 0; i < 100; i++ {
		src := "192.168.1." + string(rune('0'+i/10)) + string(rune('0'+i%10))
		dst := "10.0.0." + string(rune('0'+i/10)) + string(rune('0'+i%10))
		tracker.RecordLoss(src, dst)
	}

	assert.Equal(t, 100, tracker.Count())

	top := tracker.GetTopPairs(10)
	assert.Len(t, top, 10)
}

func TestLossTracker_Concurrent(t *testing.T) {
	tracker := NewLossTracker(5 * time.Minute)
	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.RecordLoss("192.168.1.1", "192.168.1.2")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have recorded all losses
	pair, ok := tracker.GetPair("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, uint64(1000), pair.LossCount)
}
