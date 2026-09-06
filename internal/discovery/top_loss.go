package discovery

import (
	"container/list"
	"sort"
	"sync"
	"time"
)

// LossPair represents a source-destination pair with loss statistics
type LossPair struct {
	SrcIP     string    `json:"src_ip"`
	DstIP     string    `json:"dst_ip"`
	LossCount uint64    `json:"loss_count"`
	LastSeen  time.Time `json:"last_seen"`
	buckets   [64]lossBucket
	LossRate  float64 `json:"loss_rate"` // losses per second
}

type lossBucket struct {
	Tick  int64
	Count uint64
}

// LossTracker tracks loss statistics for IP pairs
type LossTracker struct {
	mu       sync.RWMutex
	pairs    map[string]*LossPair
	window   time.Duration // Rate window, quantized into 64 bounded time buckets.
	maxPairs int
	order    *list.List
	index    map[string]*list.Element
	evicted  uint64
}

// NewLossTracker creates a new loss tracker
func NewLossTracker(window time.Duration) *LossTracker {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &LossTracker{
		maxPairs: 10000, order: list.New(), index: make(map[string]*list.Element),
		pairs:  make(map[string]*LossPair),
		window: window,
	}
}

// DefaultLossTracker creates a tracker with default 5-minute window
func DefaultLossTracker() *LossTracker {
	return NewLossTracker(5 * time.Minute)
}

// RecordLoss records a loss event for a pair
func (t *LossTracker) RecordLoss(srcIP, dstIP string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.makeKey(srcIP, dstIP)
	now := time.Now()

	if pair, ok := t.pairs[key]; ok {
		if now.Sub(pair.LastSeen) >= t.window {
			pair.buckets = [64]lossBucket{}
		}
		pair.LossCount++
		pair.LastSeen = now
		t.recordBucket(pair, now)
		t.order.MoveToFront(t.index[key])
	} else {
		if len(t.pairs) >= t.maxPairs {
			t.remove(t.order.Back().Value.(string))
			t.evicted++
		}
		t.index[key] = t.order.PushFront(key)
		t.pairs[key] = &LossPair{
			SrcIP:     srcIP,
			DstIP:     dstIP,
			LossCount: 1,
			LastSeen:  now,
		}
		t.recordBucket(t.pairs[key], now)
	}
}

// GetTopPairs returns the top N pairs by loss count
func (t *LossTracker) GetTopPairs(n int) []*LossPair {
	pairs := t.GetAllPairs()
	if n <= 0 {
		return nil
	}

	// Sort by loss count descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].LossCount > pairs[j].LossCount
	})

	// Return top N
	if len(pairs) > n {
		return pairs[:n]
	}
	return pairs
}

// GetTopPairsByRate returns the top N pairs by loss rate
func (t *LossTracker) GetTopPairsByRate(n int) []*LossPair {
	pairs := t.GetAllPairs()
	if n <= 0 {
		return nil
	}

	// Sort by loss rate descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].LossRate > pairs[j].LossRate
	})

	if len(pairs) > n {
		return pairs[:n]
	}
	return pairs
}

// GetPair returns statistics for a specific pair
func (t *LossTracker) GetPair(srcIP, dstIP string) (*LossPair, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := t.makeKey(srcIP, dstIP)
	pair, ok := t.pairs[key]
	if !ok {
		return nil, false
	}

	return t.snapshot(pair, time.Now()), true
}

// GetAllPairs returns all tracked pairs
func (t *LossTracker) GetAllPairs() []*LossPair {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pairs := make([]*LossPair, 0, len(t.pairs))
	for _, pair := range t.pairs {
		pairs = append(pairs, t.snapshot(pair, time.Now()))
	}
	return pairs
}

// Count returns the number of tracked pairs
func (t *LossTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.pairs)
}

// Cleanup removes pairs not seen within the window
func (t *LossTracker) Cleanup() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	removed := 0
	cutoff := time.Now().Add(-t.window)

	for key, pair := range t.pairs {
		if pair.LastSeen.Before(cutoff) {
			t.remove(key)
			removed++
		}
	}

	return removed
}

// Clear removes all tracked pairs
func (t *LossTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pairs = make(map[string]*LossPair)
	t.order.Init()
	t.index = make(map[string]*list.Element)
}

// makeKey creates a unique key for a pair
func (t *LossTracker) makeKey(srcIP, dstIP string) string {
	return srcIP + "->" + dstIP
}

// StartCleanup starts a background cleanup goroutine
func (t *LossTracker) StartCleanup(stopCh <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				t.Cleanup()
			}
		}
	}()
}

func (t *LossTracker) remove(key string) {
	delete(t.pairs, key)
	if e := t.index[key]; e != nil {
		t.order.Remove(e)
		delete(t.index, key)
	}
}
func (t *LossTracker) bucketWidth() time.Duration {
	width := t.window / 64
	if width < time.Nanosecond {
		return time.Nanosecond
	}
	return width
}
func (t *LossTracker) recordBucket(pair *LossPair, now time.Time) {
	tick := now.UnixNano() / int64(t.bucketWidth())
	b := &pair.buckets[tick%64]
	if b.Tick != tick {
		*b = lossBucket{Tick: tick}
	}
	b.Count++
}
func (t *LossTracker) snapshot(pair *LossPair, now time.Time) *LossPair {
	snapshot := *pair
	snapshot.LossCount = 0
	snapshot.LossRate = 0
	tick := now.UnixNano() / int64(t.bucketWidth())
	if now.Sub(pair.LastSeen) < t.window {
		for _, b := range pair.buckets {
			if b.Tick <= tick && b.Tick > tick-64 {
				snapshot.LossCount += b.Count
				snapshot.LossRate += float64(b.Count) / t.window.Seconds()
			}
		}
	}
	return &snapshot
}

// Evictions reports pairs removed by the independent cardinality limit.
func (t *LossTracker) Evictions() uint64 { t.mu.RLock(); defer t.mu.RUnlock(); return t.evicted }
