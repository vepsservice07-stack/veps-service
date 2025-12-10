package models

import (
	"time"

	"github.com/google/uuid"
)

// Event represents a normalized event from VEPS Boundary Adapter
type Event struct {
	ID          uuid.UUID              `json:"id"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Actor       Actor                  `json:"actor"`
	Evidence    map[string]interface{} `json:"evidence"`
	VectorClock VectorClock            `json:"vector_clock"`
	Metadata    EventMetadata          `json:"metadata,omitempty"`
}

// Actor represents the entity that triggered the event
type Actor struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// VectorClock tracks causality for distributed event ordering
type VectorClock map[string]int64

// EventMetadata contains additional context
type EventMetadata struct {
	ReceivedAt    time.Time `json:"received_at"`
	ProcessedAt   time.Time `json:"processed_at,omitempty"`
	BoundaryNode  string    `json:"boundary_node"`
	CorrelationID string    `json:"correlation_id"`
	RetryCount    int       `json:"retry_count"`
	SchemaVersion string    `json:"schema_version"`
}

// ContextUpdate represents the request from Boundary Adapter
type ContextUpdate struct {
	Event     Event  `json:"event"`
	Operation string `json:"operation"` // "upsert", "append", etc.
	Route     string `json:"route"`     // "rdb_updater"
}