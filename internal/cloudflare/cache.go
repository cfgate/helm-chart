// Package cloudflare provides a wrapper around cloudflare-go for cfgate's needs.
package cloudflare

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	// DefaultCacheTTL is the default TTL for cached credentials.
	DefaultCacheTTL = 30 * time.Second
)

// CredentialCache caches validated Cloudflare clients to avoid repeated API validations.
// The cache key is based on secret UID and ResourceVersion, ensuring cache invalidation
// when the secret changes.
type CredentialCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

// cacheEntry stores a cached client and its expiration time.
type cacheEntry struct {
	client    Client
	expiresAt time.Time
}

// NewCredentialCache creates a new CredentialCache with the specified TTL.
func NewCredentialCache(ttl time.Duration) *CredentialCache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &CredentialCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

// cacheKey generates a cache key from a secret's UID and ResourceVersion.
// This ensures cache invalidation when the secret is updated.
func cacheKey(secret *corev1.Secret) string {
	return string(secret.UID) + ":" + secret.ResourceVersion
}

// Get retrieves a cached client for the given secret.
// Returns nil if the entry is not found or expired.
func (c *CredentialCache) Get(secret *corev1.Secret) Client {
	key := cacheKey(secret)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		// Entry expired, remove it
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil
	}

	return entry.client
}

// Set stores a client in the cache for the given secret.
func (c *CredentialCache) Set(secret *corev1.Secret, client Client) {
	key := cacheKey(secret)

	c.mu.Lock()
	c.entries[key] = cacheEntry{
		client:    client,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// GetOrCreate retrieves a cached client or creates a new one using the provided function.
// The createFn is only called if no valid cached entry exists.
func (c *CredentialCache) GetOrCreate(ctx context.Context, secret *corev1.Secret, createFn func() (Client, error)) (Client, error) {
	// Try to get from cache first
	if client := c.Get(secret); client != nil {
		return client, nil
	}

	// Create new client
	client, err := createFn()
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.Set(secret, client)

	return client, nil
}

// Invalidate removes a specific entry from the cache.
func (c *CredentialCache) Invalidate(secret *corev1.Secret) {
	key := cacheKey(secret)

	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache.
func (c *CredentialCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

// Cleanup removes expired entries from the cache.
// This can be called periodically to prevent memory growth.
func (c *CredentialCache) Cleanup() {
	now := time.Now()

	c.mu.Lock()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

// Size returns the current number of entries in the cache.
func (c *CredentialCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
