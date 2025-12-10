package validator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/veps-service-480701/veto-service/internal/client"
	"github.com/veps-service-480701/veto-service/pkg/models"
)

// Validator performs integrity and feasibility checks on events
type Validator struct {
	rdbClient *client.RDBClient
}

// New creates a new Validator instance
func New(rdbClient *client.RDBClient) *Validator {
	return &Validator{
		rdbClient: rdbClient,
	}
}

// ValidationError represents a validation failure
type ValidationError struct {
	Check  string
	Reason string
}

// Validate performs all validation checks on an event
// Returns true if all checks pass, false with reasons if any fail
func (v *Validator) Validate(ctx context.Context, event models.Event) (bool, []ValidationError, error) {
	startTime := time.Now()
	var errors []ValidationError

	log.Printf("[Validator] Starting validation for event %s (type: %s)", event.ID, event.Type)

	// Check 1: Causality Check
	causalityPassed, causalityErr := v.checkCausality(ctx, event)
	if causalityErr != nil {
		return false, nil, fmt.Errorf("causality check error: %w", causalityErr)
	}
	if !causalityPassed.Passed {
		errors = append(errors, ValidationError{
			Check:  "causality",
			Reason: causalityPassed.Reason,
		})
	}

	// Check 2: Actor Existence Check
	actorPassed, actorErr := v.checkActorExists(ctx, event)
	if actorErr != nil {
		return false, nil, fmt.Errorf("actor check error: %w", actorErr)
	}
	if !actorPassed.Passed {
		errors = append(errors, ValidationError{
			Check:  "actor_existence",
			Reason: actorPassed.Reason,
		})
	}

	// Check 3: Business Rules Check (type-specific validation)
	businessPassed, businessErr := v.checkBusinessRules(ctx, event)
	if businessErr != nil {
		return false, nil, fmt.Errorf("business rules check error: %w", businessErr)
	}
	if !businessPassed.Passed {
		errors = append(errors, ValidationError{
			Check:  "business_rules",
			Reason: businessPassed.Reason,
		})
	}

	// Check 4: Temporal Check (timestamp sanity)
	temporalPassed, temporalErr := v.checkTemporal(ctx, event)
	if temporalErr != nil {
		return false, nil, fmt.Errorf("temporal check error: %w", temporalErr)
	}
	if !temporalPassed.Passed {
		errors = append(errors, ValidationError{
			Check:  "temporal",
			Reason: temporalPassed.Reason,
		})
	}

	duration := time.Since(startTime)
	passed := len(errors) == 0

	log.Printf("[Validator] Validation complete for event %s: passed=%v, duration=%s", 
		event.ID, passed, duration)

	return passed, errors, nil
}

// CheckResult represents the result of a single validation check
type CheckResult struct {
	Passed bool
	Reason string
}

// checkCausality verifies that all causal dependencies are satisfied
func (v *Validator) checkCausality(ctx context.Context, event models.Event) (CheckResult, error) {
	// If vector clock is empty or only has current node, no dependencies to check
	if len(event.VectorClock) <= 1 {
		return CheckResult{Passed: true}, nil
	}

	// Check if all events referenced in the vector clock exist in the database
	satisfied, missingNodes, err := v.rdbClient.CheckCausality(ctx, event.VectorClock)
	if err != nil {
		return CheckResult{Passed: false}, err
	}

	if !satisfied {
		reason := fmt.Sprintf("causal dependencies not satisfied, missing nodes: %v", missingNodes)
		log.Printf("[Validator] Causality check failed for event %s: %s", event.ID, reason)
		return CheckResult{
			Passed: false,
			Reason: reason,
		}, nil
	}

	return CheckResult{Passed: true}, nil
}

// checkActorExists verifies the actor has previous activity in the system
func (v *Validator) checkActorExists(ctx context.Context, event models.Event) (CheckResult, error) {
	// For now, we'll do a simple check: if this is not the first event from this actor,
	// they should have history. For MVP, we'll be lenient and just log.
	
	// In production, you might query for previous events from this actor
	// For now, we'll pass this check (trust but verify pattern)
	
	log.Printf("[Validator] Actor check for %s: accepting (lenient mode)", event.Actor.ID)
	return CheckResult{Passed: true}, nil
}

// checkBusinessRules applies type-specific business logic
func (v *Validator) checkBusinessRules(ctx context.Context, event models.Event) (CheckResult, error) {
	// Apply different rules based on event type
	switch event.Type {
	case "payment_processed":
		return v.validatePayment(ctx, event)
	case "user_login":
		return v.validateLogin(ctx, event)
	case "withdrawal":
		return v.validateWithdrawal(ctx, event)
	default:
		// Unknown types pass by default (permissive for MVP)
		log.Printf("[Validator] No specific business rules for type: %s", event.Type)
		return CheckResult{Passed: true}, nil
	}
}

// validatePayment checks payment-specific rules
func (v *Validator) validatePayment(ctx context.Context, event models.Event) (CheckResult, error) {
	// Check if amount exists and is positive
	amount, ok := event.Evidence["amount"].(float64)
	if !ok {
		return CheckResult{
			Passed: false,
			Reason: "payment amount is missing or invalid",
		}, nil
	}

	if amount <= 0 {
		return CheckResult{
			Passed: false,
			Reason: fmt.Sprintf("payment amount must be positive, got: %.2f", amount),
		}, nil
	}

	// Check if amount exceeds reasonable limits (e.g., $1M per transaction)
	if amount > 1000000 {
		return CheckResult{
			Passed: false,
			Reason: fmt.Sprintf("payment amount exceeds limit: %.2f", amount),
		}, nil
	}

	return CheckResult{Passed: true}, nil
}

// validateLogin checks login-specific rules
func (v *Validator) validateLogin(ctx context.Context, event models.Event) (CheckResult, error) {
	// Could check for rate limiting, suspicious IPs, etc.
	// For MVP, just accept
	return CheckResult{Passed: true}, nil
}

// validateWithdrawal checks withdrawal-specific rules
func (v *Validator) validateWithdrawal(ctx context.Context, event models.Event) (CheckResult, error) {
	// In a real system, you'd check account balance here by querying RDB
	// For MVP, we'll do a simple amount check
	amount, ok := event.Evidence["amount"].(float64)
	if !ok {
		return CheckResult{
			Passed: false,
			Reason: "withdrawal amount is missing or invalid",
		}, nil
	}

	if amount <= 0 {
		return CheckResult{
			Passed: false,
			Reason: "withdrawal amount must be positive",
		}, nil
	}

	// For demo purposes, reject withdrawals over $10,000
	if amount > 10000 {
		return CheckResult{
			Passed: false,
			Reason: fmt.Sprintf("withdrawal amount exceeds daily limit: %.2f", amount),
		}, nil
	}

	return CheckResult{Passed: true}, nil
}

// checkTemporal verifies the timestamp is reasonable
func (v *Validator) checkTemporal(ctx context.Context, event models.Event) (CheckResult, error) {
	now := time.Now().UTC()

	// Check if timestamp is too far in the past (more than 1 hour)
	if event.Timestamp.Before(now.Add(-1 * time.Hour)) {
		return CheckResult{
			Passed: false,
			Reason: "event timestamp is too old (more than 1 hour in the past)",
		}, nil
	}

	// Check if timestamp is in the future (allow 5 minute clock skew)
	if event.Timestamp.After(now.Add(5 * time.Minute)) {
		return CheckResult{
			Passed: false,
			Reason: "event timestamp is in the future",
		}, nil
	}

	return CheckResult{Passed: true}, nil
}