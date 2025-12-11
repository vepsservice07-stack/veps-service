package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// APIKey represents an API key with metadata
type APIKey struct {
	Key       string
	ClientID  string
	Name      string
	RateLimit int // requests per minute
}

// KeyStore manages API keys
type KeyStore struct {
	keys      map[string]*APIKey // hashed key -> APIKey
	projectID string
	mu        sync.RWMutex
}

// NewKeyStore creates a new key store
func NewKeyStore(projectID string) *KeyStore {
	ks := &KeyStore{
		keys:      make(map[string]*APIKey),
		projectID: projectID,
	}
	
	// Load keys from Secret Manager
	if err := ks.loadKeys(); err != nil {
		log.Printf("[Auth] Warning: Failed to load API keys: %v", err)
	}
	
	return ks
}

// loadKeys loads API keys from Secret Manager
func (ks *KeyStore) loadKeys() error {
	ctx := context.Background()
	
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()
	
	// Load API keys secret (JSON format)
	name := fmt.Sprintf("projects/%s/secrets/veps-api-keys/versions/latest", ks.projectID)
	result, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return fmt.Errorf("failed to access secret: %w", err)
	}
	
	// Parse keys (format: key1:client1:name1:rate1,key2:client2:name2:rate2)
	keysData := string(result.Payload.Data)
	entries := strings.Split(keysData, ",")
	
	for _, entry := range entries {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) != 4 {
			log.Printf("[Auth] Warning: Invalid key entry format: %s", entry)
			continue
		}
		
		key := parts[0]
		clientID := parts[1]
		name := parts[2]
		rateLimit := 100 // default
		fmt.Sscanf(parts[3], "%d", &rateLimit)
		
		// Hash the key for storage
		hashedKey := hashKey(key)
		
		ks.mu.Lock()
		ks.keys[hashedKey] = &APIKey{
			Key:       key,
			ClientID:  clientID,
			Name:      name,
			RateLimit: rateLimit,
		}
		ks.mu.Unlock()
		
		log.Printf("[Auth] Loaded API key for client: %s (%s)", clientID, name)
	}
	
	log.Printf("[Auth] Loaded %d API keys from Secret Manager", len(ks.keys))
	return nil
}

// ValidateKey checks if an API key is valid
func (ks *KeyStore) ValidateKey(key string) (*APIKey, bool) {
	hashedKey := hashKey(key)
	
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	
	apiKey, exists := ks.keys[hashedKey]
	return apiKey, exists
}

// hashKey hashes an API key for storage
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// RateLimiter tracks request rates per client
type RateLimiter struct {
	requests map[string]*clientRate
	mu       sync.RWMutex
}

type clientRate struct {
	count      int
	windowStart time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*clientRate),
	}
	
	// Clean up old entries every minute
	go rl.cleanup()
	
	return rl
}

// Allow checks if a request is allowed for a client
func (rl *RateLimiter) Allow(clientID string, limit int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	rate, exists := rl.requests[clientID]
	
	if !exists || now.Sub(rate.windowStart) > time.Minute {
		// New window
		rl.requests[clientID] = &clientRate{
			count:      1,
			windowStart: now,
		}
		return true
	}
	
	// Within window
	if rate.count >= limit {
		return false
	}
	
	rate.count++
	return true
}

// cleanup removes old rate limit entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for clientID, rate := range rl.requests {
			if now.Sub(rate.windowStart) > 2*time.Minute {
				delete(rl.requests, clientID)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware creates authentication middleware
func Middleware(keyStore *KeyStore, rateLimiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health check
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			
			// Extract API key from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, "Missing Authorization header")
				return
			}
			
			// Expected format: "Bearer <api-key>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				writeAuthError(w, "Invalid Authorization header format. Expected: Bearer <api-key>")
				return
			}
			
			apiKey := parts[1]
			
			// Validate API key
			key, valid := keyStore.ValidateKey(apiKey)
			if !valid {
				writeAuthError(w, "Invalid API key")
				return
			}
			
			// Check rate limit
			if !rateLimiter.Allow(key.ClientID, key.RateLimit) {
				writeRateLimitError(w, key.RateLimit)
				return
			}
			
			// Add client info to context
			ctx := context.WithValue(r.Context(), "client_id", key.ClientID)
			ctx = context.WithValue(ctx, "client_name", key.Name)
			
			log.Printf("[Auth] Authorized request from %s (%s)", key.ClientID, key.Name)
			
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeAuthError writes an authentication error response
func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(fmt.Sprintf(`{"success":false,"error":"%s","timestamp":"%s"}`, 
		message, time.Now().UTC().Format(time.RFC3339))))
}

// writeRateLimitError writes a rate limit error response
func writeRateLimitError(w http.ResponseWriter, limit int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(fmt.Sprintf(`{"success":false,"error":"Rate limit exceeded. Limit: %d requests per minute","timestamp":"%s"}`, 
		limit, time.Now().UTC().Format(time.RFC3339))))
}
