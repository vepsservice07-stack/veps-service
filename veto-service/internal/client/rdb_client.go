package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/veps-service-480701/veto-service/pkg/models"
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

// getIDToken gets an ID token for authenticating to another Cloud Run service
func getIDToken(ctx context.Context, audience string) (string, error) {
	return GetIDToken(ctx, audience)
}

// GetEvent retrieves an event by ID from the database
func (c *RDBClient) GetEvent(ctx context.Context, eventID string) (*models.Event, error) {
	// Build URL with query parameter
	reqURL := fmt.Sprintf("%s/event?id=%s", c.baseURL, url.QueryEscape(eventID))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication token
	token, err := getIDToken(ctx, c.baseURL)
	if err != nil {
		fmt.Printf("Warning: failed to get ID token: %v\n", err)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("event not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RDB Updater returned status %d", resp.StatusCode)
	}

	// Parse response
	var response struct {
		Success bool         `json:"success"`
		Data    models.Event `json:"data"`
		Error   string       `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("RDB Updater error: %s", response.Error)
	}

	return &response.Data, nil
}

// CheckCausality verifies vector clock causality
func (c *RDBClient) CheckCausality(ctx context.Context, vc models.VectorClock) (bool, []string, error) {
	// Prepare request body
	reqBody := struct {
		VectorClock models.VectorClock `json:"vector_clock"`
	}{
		VectorClock: vc,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/causality", c.baseURL),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return false, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication token
	token, err := getIDToken(ctx, c.baseURL)
	if err != nil {
		fmt.Printf("Warning: failed to get ID token: %v\n", err)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var response struct {
		Success bool   `json:"success"`
		Data    struct {
			Satisfied    bool     `json:"satisfied"`
			MissingNodes []string `json:"missing_nodes"`
		} `json:"data"`
		Error string `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Note: 412 Precondition Failed means causality not satisfied, which is valid
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPreconditionFailed {
		return false, nil, fmt.Errorf("RDB Updater returned unexpected status %d", resp.StatusCode)
	}

	return response.Data.Satisfied, response.Data.MissingNodes, nil
}

// CountEventsByActor counts events for a specific actor (for rate limiting checks)
func (c *RDBClient) CountEventsByActor(ctx context.Context, actorID string, since time.Time) (int, error) {
	// TODO: Implement this endpoint in RDB Updater if needed for rate limiting
	// For now, return a placeholder
	return 0, nil
}