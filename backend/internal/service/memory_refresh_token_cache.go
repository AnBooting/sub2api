package service

import (
	"context"
	"sync"
	"time"
)

// MemoryRefreshTokenCache is a mutex-guarded RefreshTokenCache used by unit
// tests and embedded/offline deployments. It intentionally mirrors the Redis
// implementation's key domains rather than sharing state between them.
type MemoryRefreshTokenCache struct {
	mu          sync.RWMutex
	tokens      map[string]*RefreshTokenData
	userSets    map[int64]map[string]struct{}
	families    map[string]map[string]struct{}
	consumed    map[string]string
	consumedAt  map[string]time.Time
	consumedTTL map[string]time.Duration
}

// NewMemoryRefreshTokenCache creates an empty in-memory refresh token cache.
func NewMemoryRefreshTokenCache() *MemoryRefreshTokenCache {
	return &MemoryRefreshTokenCache{
		tokens:      make(map[string]*RefreshTokenData),
		userSets:    make(map[int64]map[string]struct{}),
		families:    make(map[string]map[string]struct{}),
		consumed:    make(map[string]string),
		consumedAt:  make(map[string]time.Time),
		consumedTTL: make(map[string]time.Duration),
	}
}

func (c *MemoryRefreshTokenCache) StoreRefreshToken(_ context.Context, tokenHash string, data *RefreshTokenData, ttl time.Duration) error {
	if c == nil || data == nil {
		return ErrServiceUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := *data
	c.tokens[tokenHash] = &cloned
	if c.families[cloned.FamilyID] == nil {
		c.families[cloned.FamilyID] = make(map[string]struct{})
	}
	c.families[cloned.FamilyID][tokenHash] = struct{}{}
	if c.userSets[cloned.UserID] == nil {
		c.userSets[cloned.UserID] = make(map[string]struct{})
	}
	c.userSets[cloned.UserID][tokenHash] = struct{}{}
	_ = ttl
	return nil
}

func (c *MemoryRefreshTokenCache) GetRefreshToken(_ context.Context, tokenHash string) (*RefreshTokenData, error) {
	if c == nil {
		return nil, ErrServiceUnavailable
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.tokens[tokenHash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	cloned := *data
	return &cloned, nil
}

func (c *MemoryRefreshTokenCache) DeleteRefreshToken(_ context.Context, tokenHash string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, tokenHash)
	for _, set := range c.userSets {
		delete(set, tokenHash)
	}
	for _, set := range c.families {
		delete(set, tokenHash)
	}
	return nil
}

func (c *MemoryRefreshTokenCache) DeleteUserRefreshTokens(_ context.Context, userID int64) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for tokenHash := range c.userSets[userID] {
		delete(c.tokens, tokenHash)
		for _, set := range c.families {
			delete(set, tokenHash)
		}
	}
	delete(c.userSets, userID)
	return nil
}

func (c *MemoryRefreshTokenCache) DeleteTokenFamily(_ context.Context, familyID string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for tokenHash := range c.families[familyID] {
		delete(c.tokens, tokenHash)
		for _, set := range c.userSets {
			delete(set, tokenHash)
		}
		delete(c.consumed, tokenHash)
		delete(c.consumedAt, tokenHash)
		delete(c.consumedTTL, tokenHash)
	}
	delete(c.families, familyID)
	return nil
}

func (c *MemoryRefreshTokenCache) AddToUserTokenSet(_ context.Context, userID int64, tokenHash string, _ time.Duration) error {
	if c == nil {
		return ErrServiceUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.userSets[userID] == nil {
		c.userSets[userID] = make(map[string]struct{})
	}
	c.userSets[userID][tokenHash] = struct{}{}
	return nil
}

func (c *MemoryRefreshTokenCache) AddToFamilyTokenSet(_ context.Context, familyID string, tokenHash string, _ time.Duration) error {
	if c == nil {
		return ErrServiceUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.families[familyID] == nil {
		c.families[familyID] = make(map[string]struct{})
	}
	c.families[familyID][tokenHash] = struct{}{}
	return nil
}

func (c *MemoryRefreshTokenCache) GetUserTokenHashes(_ context.Context, userID int64) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.userSets[userID]))
	for tokenHash := range c.userSets[userID] {
		out = append(out, tokenHash)
	}
	return out, nil
}

func (c *MemoryRefreshTokenCache) GetFamilyTokenHashes(_ context.Context, familyID string) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.families[familyID]))
	for tokenHash := range c.families[familyID] {
		out = append(out, tokenHash)
	}
	return out, nil
}

func (c *MemoryRefreshTokenCache) IsTokenInFamily(_ context.Context, familyID string, tokenHash string) (bool, error) {
	if c == nil {
		return false, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.families[familyID][tokenHash]
	return ok, nil
}

func (c *MemoryRefreshTokenCache) MarkRefreshTokenConsumed(_ context.Context, tokenHash string, familyID string, ttl time.Duration) error {
	if c == nil {
		return ErrServiceUnavailable
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumed[tokenHash] = familyID
	c.consumedAt[tokenHash] = time.Now()
	c.consumedTTL[tokenHash] = ttl
	return nil
}

func (c *MemoryRefreshTokenCache) GetConsumedRefreshTokenFamilyID(_ context.Context, tokenHash string) (string, error) {
	if c == nil {
		return "", ErrServiceUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	deadline, ok := c.consumedAt[tokenHash]
	if !ok {
		return "", ErrConsumedRefreshTokenNotFound
	}
	if time.Since(deadline) >= c.consumedTTL[tokenHash] {
		delete(c.consumed, tokenHash)
		delete(c.consumedAt, tokenHash)
		delete(c.consumedTTL, tokenHash)
		return "", ErrConsumedRefreshTokenNotFound
	}
	return c.consumed[tokenHash], nil
}
