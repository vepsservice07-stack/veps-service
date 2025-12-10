package models

import (
	"time"

	"github.com/google/uuid"
)

// Event represents a normalized event in the VEPS system
// This is the canonical format after boundary adapter normalization
type Event struct {
	ID           uuid.UUID         `json:"id"`
	Type         string            `json:"type"`
	Source       string            `json:"source"`
	Timestamp    time.Time         `json:"timestamp"`
	Actor        Actor             `json:"actor"`
	Evidence     map[string]any    `json:"evidence"`
	VectorClock  VectorClock       `json:"vector_clock"`
	Metadata     EventMetadata     `json:"metadata,omitempty"`
}

// Actor represents the entity that triggered the event
type Actor struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type,omitempty"` // e.g., "user", "service", "system"
	Metadata map[string]string `json:"metadata,omitempty"`
}

// VectorClock tracks causality for distributed event ordering
// Maps node/service ID to logical clock value
type VectorClock map[string]int64

// EventMetadata contains additional context for tracking and debugging
type EventMetadata struct {
	ReceivedAt      time.Time `json:"received_at"`
	ProcessedAt     time.Time `json:"processed_at,omitempty"`
	BoundaryNode    string    `json:"boundary_node"`     // Which VEPS instance processed this
	CorrelationID   string    `json:"correlation_id"`    // For distributed tracing
	RetryCount      int       `json:"retry_count"`
	SchemaVersion   string    `json:"schema_version"`
}

// RawEvent represents the incoming event before normalization
type RawEvent struct {
	Data      map[string]any `json:"data"`
	Source    string         `json:"source"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
}

// IntegrityCheckResult is sent down the integrity path to Veto Service
type IntegrityCheckResult struct {
	Event       Event  `json:"event"`
	CheckStatus string `json:"check_status"` // "pending", "passed", "failed"
	Route       string `json:"route"`        // "veto_service"
}

// ContextUpdate is sent down the context path to RDB Updater
type ContextUpdate struct {
	Event     Event  `json:"event"`
	Operation string `json:"operation"` // "upsert", "append", etc.
	Route     string `json:"route"`     // "rdb_updater"
}

// Increment increments the vector clock for the given node
func (vc VectorClock) Increment(nodeID string) {
	vc[nodeID]++
}

// Merge merges another vector clock into this one (takes max of each entry)
func (vc VectorClock) Merge(other VectorClock) {
	for nodeID, timestamp := range other {
		if vc[nodeID] < timestamp {
			vc[nodeID] = timestamp
		}
	}
}

// HappensBefore checks if this vector clock happens before another
func (vc VectorClock) HappensBefore(other VectorClock) bool {
	lessThanOrEqual := false
	strictlyLess := false

	for nodeID, timestamp := range vc {
		otherTimestamp, exists := other[nodeID]
		if !exists || timestamp > otherTimestamp {
			return false
		}
		if timestamp < otherTimestamp {
			strictlyLess = true
		}
		lessThanOrEqual = true
	}

	return lessThanOrEqual && strictlyLess
}

// IsConcurrent checks if two vector clocks are concurrent (neither happens before the other)
func (vc VectorClock) IsConcurrent(other VectorClock) bool {
	return !vc.HappensBefore(other) && !other.HappensBefore(vc)
}