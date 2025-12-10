package router

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/veps-service-480701/boundary-adapter/pkg/models"
)

// Router handles the concurrent split of normalized events
// Sends to both Integrity Path (Veto Service) and Context Path (RDB Updater)
type Router struct {
	integrityHandler IntegrityHandler
	contextHandler   ContextHandler
	timeout          time.Duration
}

// IntegrityHandler defines the interface for sending to Veto Service
type IntegrityHandler interface {
	SendToVeto(ctx context.Context, event models.Event) error
}

// ContextHandler defines the interface for sending to RDB Updater
type ContextHandler interface {
	SendToRDB(ctx context.Context, event models.Event) error
}

// New creates a new Router with the specified handlers
func New(integrity IntegrityHandler, context ContextHandler, timeout time.Duration) *Router {
	if timeout == 0 {
		timeout = 10 * time.Second // Default timeout
	}

	return &Router{
		integrityHandler: integrity,
		contextHandler:   context,
		timeout:          timeout,
	}
}

// RouteResult contains the outcome of routing an event
type RouteResult struct {
	Event            models.Event
	IntegritySuccess bool
	IntegrityError   error
	ContextSuccess   bool
	ContextError     error
	Duration         time.Duration
}

// Route performs the concurrent split - sends event to both paths simultaneously
// The Integrity Path is BLOCKING (we wait for veto decision)
// The Context Path is NON-BLOCKING (fire and forget)
func (r *Router) Route(ctx context.Context, event models.Event) (*RouteResult, error) {
	startTime := time.Now()

	result := &RouteResult{
		Event: event,
	}

	// Create a context with timeout for the entire routing operation
	routeCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// WaitGroup for concurrent operations
	var wg sync.WaitGroup
	wg.Add(2)

	// Channel to capture integrity path result (blocking requirement)
	integrityDone := make(chan error, 1)

	// INTEGRITY PATH - Critical, blocking
	go func() {
		defer wg.Done()
		err := r.integrityHandler.SendToVeto(routeCtx, event)
		if err != nil {
			log.Printf("[Router] Integrity path failed for event %s: %v", event.ID, err)
			result.IntegrityError = err
		} else {
			result.IntegritySuccess = true
		}
		integrityDone <- err
	}()

	// CONTEXT PATH - Non-blocking, fire and forget
	go func() {
		defer wg.Done()
		// Use a separate context that won't be cancelled if integrity fails
		contextCtx := context.Background()
		err := r.contextHandler.SendToRDB(contextCtx, event)
		if err != nil {
			// Log but don't fail the overall operation
			log.Printf("[Router] Context path failed for event %s (non-blocking): %v", event.ID, err)
			result.ContextError = err
		} else {
			result.ContextSuccess = true
		}
	}()

	// CRITICAL: Wait for integrity path to complete
	// This enforces the blocking requirement for veto decision
	select {
	case err := <-integrityDone:
		if err != nil {
			// Integrity path failed - this is a critical failure
			result.Duration = time.Since(startTime)
			return result, fmt.Errorf("integrity path failed: %w", err)
		}
	case <-routeCtx.Done():
		// Timeout exceeded
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("integrity path timeout exceeded: %w", routeCtx.Err())
	}

	// Wait for context path to complete (for logging purposes only)
	// We don't fail if context path has issues
	wg.Wait()

	result.Duration = time.Since(startTime)

	// Success if integrity path succeeded (context path failure is tolerated)
	if !result.IntegritySuccess {
		return result, fmt.Errorf("integrity validation failed")
	}

	return result, nil
}

// RouteBatch routes multiple events concurrently with rate limiting
func (r *Router) RouteBatch(ctx context.Context, events []models.Event, maxConcurrent int) []*RouteResult {
	if maxConcurrent <= 0 {
		maxConcurrent = 10 // Default concurrency limit
	}

	results := make([]*RouteResult, len(events))
	semaphore := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, event := range events {
		wg.Add(1)
		go func(idx int, evt models.Event) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result, err := r.Route(ctx, evt)
			if err != nil {
				log.Printf("[Router] Batch routing failed for event %s: %v", evt.ID, err)
			}
			results[idx] = result
		}(i, event)
	}

	wg.Wait()
	return results
}

// MockIntegrityHandler is a mock implementation for testing
type MockIntegrityHandler struct {
	Delay time.Duration
	Fail  bool
}

func (m *MockIntegrityHandler) SendToVeto(ctx context.Context, event models.Event) error {
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if m.Fail {
		return fmt.Errorf("mock integrity handler failure")
	}

	log.Printf("[MockIntegrity] Event %s sent to veto service", event.ID)
	return nil
}

// MockContextHandler is a mock implementation for testing
type MockContextHandler struct {
	Delay time.Duration
	Fail  bool
}

func (m *MockContextHandler) SendToRDB(ctx context.Context, event models.Event) error {
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if m.Fail {
		return fmt.Errorf("mock context handler failure")
	}

	log.Printf("[MockContext] Event %s sent to RDB updater", event.ID)
	return nil
}