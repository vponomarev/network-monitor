package discovery

import (
	"sort"
	"sync"
	"time"
)

// LossPair represents a source-destination pair with loss statistics
type LossPair struct {
	SrcIP       string    `json:"src_ip"`
	DstIP       string    `json:"dst_ip"`
	LossCount   uint64    `json:"loss_count"`
	LastSeen    time.Time `json:"last_seen"`
	LossRate    float64   `json:"loss_rate"` // losses per second
}

// LossTracker tracks loss statistics for IP pairs
type LossTracker struct {
	mu     sync.RWMutex
	pairs  map[string]*LossPair
	window time.Duration // Time window for rate calculation
}

// NewLossTracker creates a new loss tracker
func NewLossTracker(window time.Duration) *LossTracker {
	return &LossTracker{
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
		pair.LossCount++
		pair.LastSeen = now
		pair.LossRate = float64(pair.LossCount) / t.window.Seconds()
	} else {
		t.pairs[key] = &LossPair{
			SrcIP:     srcIP,
			DstIP:     dstIP,
			LossCount: 1,
			LastSeen:  now,
			LossRate:  1.0 / t.window.Seconds(),
		}
	}
}

// GetTopPairs returns the top N pairs by loss count
func (t *LossTracker) GetTopPairs(n int) []*LossPair {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pairs := make([]*LossPair, 0, len(t.pairs))
	for _, pair := range t.pairs {
		pairs = append(pairs, pair)
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
	t.mu.RLock()
	defer t.mu.RUnlock()

	pairs := make([]*LossPair, 0, len(t.pairs))
	for _, pair := range t.pairs {
		pairs = append(pairs, pair)
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

	// Return a copy
	return &LossPair{
		SrcIP:     pair.SrcIP,
		DstIP:     pair.DstIP,
		LossCount: pair.LossCount,
		LastSeen:  pair.LastSeen,
		LossRate:  pair.LossRate,
	}, true
}

// GetAllPairs returns all tracked pairs
func (t *LossTracker) GetAllPairs() []*LossPair {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pairs := make([]*LossPair, 0, len(t.pairs))
	for _, pair := range t.pairs {
		pairs = append(pairs, pair)
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
			delete(t.pairs, key)
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
}

// makeKey creates a unique key for a pair
func (t *LossTracker) makeKey(srcIP, dstIP string) string {
	return srcIP + "->" + dstIP
}

// StartCleanup starts a background cleanup goroutine
func (t *LossTracker) StartCleanup(stopCh <-chan struct{}, interval time.Duration) {
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
