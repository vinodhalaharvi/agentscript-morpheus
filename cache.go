package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Cache provides simple file-based caching for API results
type Cache struct {
	dir     string
	verbose bool
}

// CacheEntry wraps cached data with metadata
type CacheEntry struct {
	Data      string    `json:"data"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	TTL       int       `json:"ttl_seconds"`
}

// NewCache creates a new file cache
func NewCache(verbose bool) *Cache {
	// Default cache directory
	dir := os.Getenv("AGENTSCRIPT_CACHE_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/tmp"
		}
		dir = filepath.Join(home, ".agentscript", "cache")
	}

	// Create cache directory if needed
	os.MkdirAll(dir, 0755)

	return &Cache{
		dir:     dir,
		verbose: verbose,
	}
}

func (c *Cache) log(format string, args ...any) {
	if c.verbose {
		fmt.Printf("[CACHE] "+format+"\n", args...)
	}
}

// cacheKey generates a filename-safe cache key
func (c *Cache) cacheKey(namespace, key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, fmt.Sprintf("%s_%x.json", namespace, hash[:8]))
}

// Get retrieves a cached value if it exists and hasn't expired
func (c *Cache) Get(namespace, key string) (string, bool) {
	path := c.cacheKey(namespace, key)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.log("Cache corrupt for %s/%s, removing", namespace, key)
		os.Remove(path)
		return "", false
	}

	// Check TTL
	elapsed := time.Since(entry.CreatedAt)
	if elapsed > time.Duration(entry.TTL)*time.Second {
		c.log("Cache expired for %s/%s (age: %v, ttl: %ds)", namespace, key, elapsed, entry.TTL)
		os.Remove(path)
		return "", false
	}

	c.log("Cache HIT for %s/%s (age: %v)", namespace, key, elapsed.Round(time.Second))
	return entry.Data, true
}

// Set stores a value in the cache with a TTL
func (c *Cache) Set(namespace, key, data string, ttlSeconds int) error {
	entry := CacheEntry{
		Data:      data,
		Key:       key,
		CreatedAt: time.Now(),
		TTL:       ttlSeconds,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	path := c.cacheKey(namespace, key)
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	c.log("Cache SET for %s/%s (ttl: %ds)", namespace, key, ttlSeconds)
	return nil
}

// Invalidate removes a cached entry
func (c *Cache) Invalidate(namespace, key string) {
	path := c.cacheKey(namespace, key)
	os.Remove(path)
	c.log("Cache INVALIDATED for %s/%s", namespace, key)
}

// Clear removes all cached entries
func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			os.Remove(filepath.Join(c.dir, entry.Name()))
			count++
		}
	}

	c.log("Cache CLEARED (%d entries removed)", count)
	return nil
}

// Stats returns cache statistics
func (c *Cache) Stats() string {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return "Cache directory not found"
	}

	count := 0
	var totalSize int64
	expired := 0

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++

		info, err := entry.Info()
		if err == nil {
			totalSize += info.Size()
		}

		// Check if expired
		data, err := os.ReadFile(filepath.Join(c.dir, entry.Name()))
		if err == nil {
			var ce CacheEntry
			if json.Unmarshal(data, &ce) == nil {
				if time.Since(ce.CreatedAt) > time.Duration(ce.TTL)*time.Second {
					expired++
				}
			}
		}
	}

	return fmt.Sprintf("Cache: %d entries (%d expired), %.1f KB, dir: %s",
		count, expired, float64(totalSize)/1024, c.dir)
}

// Default TTLs for different data types
const (
	CacheTTLStock   = 60   // 1 minute — stock prices change fast
	CacheTTLCrypto  = 60   // 1 minute
	CacheTTLNews    = 300  // 5 minutes
	CacheTTLWeather = 600  // 10 minutes
	CacheTTLJobs    = 3600 // 1 hour — jobs don't change that fast
	CacheTTLReddit  = 300  // 5 minutes
	CacheTTLRSS     = 600  // 10 minutes
	CacheTTLSearch  = 1800 // 30 minutes
)

// CachedGet is a helper that checks cache first, then calls the fetch function
func CachedGet(cache *Cache, namespace string, key string, ttl int, fetch func() (string, error)) (string, error) {
	if cache != nil {
		if data, ok := cache.Get(namespace, key); ok {
			return data, nil
		}
	}

	// Cache miss — fetch fresh data
	data, err := fetch()
	if err != nil {
		return "", err
	}

	// Store in cache
	if cache != nil {
		cache.Set(namespace, key, data, ttl)
	}

	return data, nil
}
