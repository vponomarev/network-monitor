package discovery

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestAuditTopSnapshot(t *testing.T) {
	tracker := NewLossTracker(time.Second)
	tracker.RecordLoss("a", "b")
	snapshot := tracker.GetTopPairs(1)
	tracker.RecordLoss("a", "b")
	if snapshot[0].LossCount != 1 {
		t.Fatalf("returned snapshot changed after unlock: %d", snapshot[0].LossCount)
	}
}

func TestAuditWindowRate(t *testing.T) {
	tracker := NewLossTracker(time.Second)
	tracker.RecordLoss("a", "b")
	tracker.pairs["a->b"].LastSeen = time.Now().Add(-time.Hour)
	tracker.RecordLoss("a", "b")
	got, _ := tracker.GetPair("a", "b")
	if got.LossCount != 1 {
		t.Fatalf("expired event included in one-second window: count=%d rate=%v", got.LossCount, got.LossRate)
	}
}

func TestAuditTopRace(t *testing.T) {
	tracker := NewLossTracker(time.Second)
	tracker.RecordLoss("a", "b")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			tracker.RecordLoss("a", "b")
		}
	}()
	for i := 0; i < 10000; i++ {
		_, _ = json.Marshal(tracker.GetTopPairs(1))
	}
	wg.Wait()
}

func TestLossTrackerBoundsAndExpiresPairs(t *testing.T) {
	tracker := NewLossTracker(time.Second)
	tracker.maxPairs = 2
	tracker.RecordLoss("a", "b")
	tracker.RecordLoss("c", "d")
	tracker.RecordLoss("a", "b") // Keep the active pair.
	tracker.RecordLoss("e", "f")
	if tracker.Count() != 2 || tracker.Evictions() != 1 {
		t.Fatal("cardinality cap not enforced")
	}
	if _, ok := tracker.GetPair("c", "d"); ok {
		t.Fatal("oldest pair retained")
	}
	for _, pair := range tracker.pairs {
		pair.LastSeen = time.Now().Add(-time.Hour)
	}
	if tracker.Cleanup() != 2 || tracker.order.Len() != 0 || len(tracker.index) != 0 {
		t.Fatal("cleanup leaves retained state")
	}
}
