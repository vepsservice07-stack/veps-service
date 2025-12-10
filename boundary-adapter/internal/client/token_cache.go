package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
)

// TokenCache caches ID tokens with automatic refresh
type TokenCache struct {
	mu            sync.RWMutex
	tokens        map[string]*cachedToken
	tokenSource   map[string]oauth2.TokenSource
	refreshBuffer time.Duration
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

var globalTokenCache = &TokenCache{
	tokens:        make(map[string]*cachedToken),
	tokenSource:   make(map[string]oauth2.TokenSource),
	refreshBuffer: 5 * time.Minute, // Refresh 5 minutes before expiry
}

// GetIDToken gets a cached or fresh ID token
func GetIDToken(ctx context.Context, audience string) (string, error) {
	cache := globalTokenCache

	// Check if we have a valid cached token
	cache.mu.RLock()
	if cached, ok := cache.tokens[audience]; ok {
		if time.Now().Before(cached.expiresAt.Add(-cache.refreshBuffer)) {
			token := cached.token
			cache.mu.RUnlock()
			return token, nil
		}
	}
	cache.mu.RUnlock()

	// Need to get a fresh token
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := cache.tokens[audience]; ok {
		if time.Now().Before(cached.expiresAt.Add(-cache.refreshBuffer)) {
			return cached.token, nil
		}
	}

	// Get or create token source
	ts, ok := cache.tokenSource[audience]
	if !ok {
		newTS, err := idtoken.NewTokenSource(ctx, audience)
		if err != nil {
			return "", fmt.Errorf("failed to create token source: %w", err)
		}
		ts = newTS
		cache.tokenSource[audience] = ts
	}

	// Get fresh token
	token, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	// Cache the token (ID tokens typically expire in 1 hour)
	cache.tokens[audience] = &cachedToken{
		token:     token.AccessToken,
		expiresAt: time.Now().Add(55 * time.Minute), // Conservative 55-minute expiry
	}

	return token.AccessToken, nil
}