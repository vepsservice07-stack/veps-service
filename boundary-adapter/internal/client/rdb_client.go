package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/veps-service-480701/boundary-adapter/pkg/models"
)

// RDBClient handles communication with the RDB Updater service
type RDBClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRDBClient creates a new RDB Updater client
func NewRDBClient(baseURL string, timeout time.Duration) *RDBClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Configure HTTP client for high performance
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   true,
	}

	return &RDBClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// ContextUpdate represents the request to RDB Updater
type ContextUpdate struct {
	Event     models.Event `json:"event"`
	Operation string       `json:"operation"`
	Route     string       `json:"route"`
}

// getIDToken gets an ID token for authenticating to another Cloud Run service
// DEPRECATED: Use GetIDToken from token_cache.go instead
func getIDToken(ctx context.Context, audience string) (string, error) {
	return GetIDToken(ctx, audience)
}

// SendToRDB sends an event to the RDB Updater (Context Path)
func (c *RDBClient) SendToRDB(ctx context.Context, event models.Event) error {
	// Prepare the context update request
	update := ContextUpdate{
		Event:     event,
		Operation: "upsert",
		Route:     "rdb_updater",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal context update: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/update", c.baseURL),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication token for Cloud Run service-to-service calls
	token, err := getIDToken(ctx, c.baseURL)
	if err != nil {
		// Log but don't fail - might be running locally without auth
		// In production, you'd want this to fail
		fmt.Printf("Warning: failed to get ID token: %v\n", err)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			if errMsg, ok := errorResp["error"].(string); ok {
				return fmt.Errorf("RDB Updater error: %s", errMsg)
			}
		}
		return fmt.Errorf("RDB Updater returned status %d", resp.StatusCode)
	}

	return nil
}

// VetoClient handles communication with the Veto Service (placeholder for now)
type VetoClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewVetoClient creates a new Veto Service client
func NewVetoClient(baseURL string, timeout time.Duration) *VetoClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Configure HTTP client for high performance
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   true,
	}

	return &VetoClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// SendToVeto sends an event to the Veto Service for validation (Integrity Path)
func (c *VetoClient) SendToVeto(ctx context.Context, event models.Event) error {
	// Prepare the veto request
	vetoRequest := struct {
		Event models.Event `json:"event"`
		Route string       `json:"route"`
	}{
		Event: event,
		Route: "veto_service",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(vetoRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal veto request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/validate", c.baseURL),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication token for Cloud Run service-to-service calls
	token, err := getIDToken(ctx, c.baseURL)
	if err != nil {
		// Log but don't fail - might be running locally without auth
		fmt.Printf("Warning: failed to get ID token: %v\n", err)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	// 200 = validation passed, 412 = validation failed (vetoed)
	if resp.StatusCode == http.StatusOK {
		return nil // Validation passed
	}

	if resp.StatusCode == http.StatusPreconditionFailed {
		// Parse the veto response to get details
		var errorResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			if data, ok := errorResp["data"].(map[string]interface{}); ok {
				if reasons, ok := data["reasons"].([]interface{}); ok {
					return fmt.Errorf("event vetoed: %v", reasons)
				}
			}
		}
		return fmt.Errorf("event vetoed by Veto Service")
	}

	return fmt.Errorf("Veto Service returned unexpected status %d", resp.StatusCode)
}