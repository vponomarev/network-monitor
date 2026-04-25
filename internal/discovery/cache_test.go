package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPathCache(t *testing.T) {
	cache := NewPathCache(5*time.Minute, 100)
	require.NotNil(t, cache)
	assert.Equal(t, 0, cache.Count())
}

func TestDefaultPathCache(t *testing.T) {
	cache := DefaultPathCache()
	require.NotNil(t, cache)
	assert.Equal(t, 0, cache.Count())
}

func TestPathCache_SetAndGet(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	path := &Path{
		SrcIP:      mustParseIP("192.168.1.1"),
		DstIP:      mustParseIP("192.168.1.2"),
		Hops:       []Hop{{TTL: 1, IP: mustParseIP("10.0.0.1")}},
		Discovered: time.Now(),
	}

	cache.Set(path)
	assert.Equal(t, 1, cache.Count())

	cached, ok := cache.Get("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, path.SrcIP, cached.SrcIP)
	assert.Equal(t, path.DstIP, cached.DstIP)
}

func TestPathCache_GetExpired(t *testing.T) {
	cache := NewPathCache(1*time.Millisecond, 100)

	path := &Path{
		SrcIP:      mustParseIP("192.168.1.1"),
		DstIP:      mustParseIP("192.168.1.2"),
		Discovered: time.Now(),
	}

	cache.Set(path)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, ok := cache.Get("192.168.1.1", "192.168.1.2")
	assert.False(t, ok)
}

func TestPathCache_Delete(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	path := &Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
	}
	cache.Set(path)
	assert.Equal(t, 1, cache.Count())

	cache.Delete("192.168.1.1", "192.168.1.2")
	assert.Equal(t, 0, cache.Count())
}

func TestPathCache_Clear(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	cache.Set(&Path{SrcIP: mustParseIP("192.168.1.1"), DstIP: mustParseIP("192.168.1.2")})
	cache.Set(&Path{SrcIP: mustParseIP("192.168.1.3"), DstIP: mustParseIP("192.168.1.4")})
	assert.Equal(t, 2, cache.Count())

	cache.Clear()
	assert.Equal(t, 0, cache.Count())
}

func TestPathCache_Cleanup(t *testing.T) {
	cache := NewPathCache(1*time.Millisecond, 100)

	// Add expired path
	cache.Set(&Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
		Discovered: time.Now().Add(-1 * time.Hour), // Expired
	})

	// Add valid path
	cache.Set(&Path{
		SrcIP: mustParseIP("192.168.1.3"),
		DstIP: mustParseIP("192.168.1.4"),
		Discovered: time.Now(),
	})

	assert.Equal(t, 2, cache.Count())

	removed := cache.Cleanup()
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, cache.Count())
}

func TestPathCache_StartCleanup(t *testing.T) {
	cache := NewPathCache(10*time.Millisecond, 100)
	ctx, cancel := context.WithCancel(context.Background())

	cache.StartCleanup(ctx, 5*time.Millisecond)

	// Add expired path
	cache.Set(&Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
		Discovered: time.Now().Add(-1 * time.Hour),
	})

	// Wait for cleanup
	time.Sleep(20 * time.Millisecond)

	assert.Equal(t, 0, cache.Count())

	cancel()
}

func TestPathCache_GetAll(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	path1 := &Path{SrcIP: mustParseIP("192.168.1.1"), DstIP: mustParseIP("192.168.1.2"), Discovered: time.Now()}
	path2 := &Path{SrcIP: mustParseIP("192.168.1.3"), DstIP: mustParseIP("192.168.1.4"), Discovered: time.Now()}
	
	cache.Set(path1)
	cache.Set(path2)

	paths := cache.GetAll()
	assert.Len(t, paths, 2)
}

func TestPathCache_GetOrLoad_FromCache(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	// Pre-populate cache
	path := &Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
		Discovered: time.Now(),
	}
	cache.Set(path)

	loadCalled := false
	loader := func() (*Path, error) {
		loadCalled = true
		return nil, nil
	}

	result, err := cache.GetOrLoad(context.Background(), "192.168.1.1", "192.168.1.2", loader)
	require.NoError(t, err)
	assert.False(t, loadCalled)
	assert.Equal(t, path, result)
}

func TestPathCache_GetOrLoad_FromLoader(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	loadCalled := false
	expectedPath := &Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
		Discovered: time.Now(),
	}
	loader := func() (*Path, error) {
		loadCalled = true
		return expectedPath, nil
	}

	result, err := cache.GetOrLoad(context.Background(), "192.168.1.1", "192.168.1.2", loader)
	require.NoError(t, err)
	assert.True(t, loadCalled)
	assert.Equal(t, expectedPath, result)

	// Verify it's now cached
	cached, ok := cache.Get("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, expectedPath, cached)
}

func TestPathCache_GetOrLoad_LoaderError(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 100)

	loader := func() (*Path, error) {
		return nil, assert.AnError
	}

	result, err := cache.GetOrLoad(context.Background(), "192.168.1.1", "192.168.1.2", loader)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestPathCache_Eviction(t *testing.T) {
	cache := NewPathCache(10*time.Minute, 2) // Max size 2

	cache.Set(&Path{SrcIP: mustParseIP("1.1.1.1"), DstIP: mustParseIP("2.2.2.2")})
	cache.Set(&Path{SrcIP: mustParseIP("3.3.3.3"), DstIP: mustParseIP("4.4.4.4")})
	assert.Equal(t, 2, cache.Count())

	// This should trigger eviction
	cache.Set(&Path{SrcIP: mustParseIP("5.5.5.5"), DstIP: mustParseIP("6.6.6.6")})
	assert.Equal(t, 2, cache.Count()) // Still at max
}

// Helper function
func mustParseIP(s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		panic("invalid IP: " + s)
	}
	return ip
}
