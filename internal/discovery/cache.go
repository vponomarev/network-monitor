package discovery

import (
	"context"
	"sync"
	"time"
)

// PathCache caches discovered paths with TTL
type PathCache struct {
	mu      sync.RWMutex
	paths   map[string]*Path
	ttl     time.Duration
	maxSize int
}

// NewPathCache creates a new path cache
func NewPathCache(ttl time.Duration, maxSize int) *PathCache {
	return &PathCache{
		paths:   make(map[string]*Path),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// DefaultPathCache creates a cache with default settings
func DefaultPathCache() *PathCache {
	return NewPathCache(10*time.Minute, 1000)
}

// Get retrieves a path from cache
func (c *PathCache) Get(srcIP, dstIP string) (*Path, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.makeKey(srcIP, dstIP)
	path, ok := c.paths[key]
	if !ok {
		return nil, false
	}

	// Check if expired
	if time.Since(path.Discovered) > c.ttl {
		return nil, false
	}

	return path, true
}

// Set stores a path in cache
func (c *PathCache) Set(path *Path) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.paths) >= c.maxSize {
		c.evictOldest()
	}

	key := c.makeKey(path.SrcIP.String(), path.DstIP.String())
	c.paths[key] = path
}

// evictOldest removes the oldest entry (by discovery time)
func (c *PathCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, path := range c.paths {
		if oldestKey == "" || path.Discovered.Before(oldestTime) {
			oldestKey = key
			oldestTime = path.Discovered
		}
	}

	if oldestKey != "" {
		delete(c.paths, oldestKey)
	}
}

// Delete removes a path from cache
func (c *PathCache) Delete(srcIP, dstIP string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.makeKey(srcIP, dstIP)
	delete(c.paths, key)
}

// Clear removes all paths from cache
func (c *PathCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paths = make(map[string]*Path)
}

// Count returns the number of cached paths
func (c *PathCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.paths)
}

// Cleanup removes expired paths
func (c *PathCache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for key, path := range c.paths {
		if time.Since(path.Discovered) > c.ttl {
			delete(c.paths, key)
			removed++
		}
	}

	return removed
}

// StartCleanup starts a background cleanup goroutine
func (c *PathCache) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Cleanup()
			}
		}
	}()
}

// GetAll returns all cached paths
func (c *PathCache) GetAll() []*Path {
	c.mu.RLock()
	defer c.mu.RUnlock()

	paths := make([]*Path, 0, len(c.paths))
	for _, path := range c.paths {
		// Only return non-expired
		if time.Since(path.Discovered) <= c.ttl {
			paths = append(paths, path)
		}
	}

	return paths
}

// makeKey creates a cache key from source and destination IPs
func (c *PathCache) makeKey(srcIP, dstIP string) string {
	return srcIP + "->" + dstIP
}

// GetOrLoad gets a path from cache or loads it using the provided loader
func (c *PathCache) GetOrLoad(
	ctx context.Context,
	srcIP, dstIP string,
	loader func() (*Path, error),
) (*Path, error) {
	// Try cache first
	if path, ok := c.Get(srcIP, dstIP); ok {
		return path, nil
	}

	// Load from source
	path, err := loader()
	if err != nil {
		return nil, err
	}

	// Cache the result
	c.Set(path)

	return path, nil
}
