package models

import (
	"time"

	"github.com/google/uuid"
)

// Event represents a normalized event from VEPS
type Event struct {
	ID          uuid.UUID              `json:"id"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Actor       Actor                  `json:"actor"`
	Evidence    map[string]interface{} `json:"evidence"`
	VectorClock map[string]int64       `json:"vector_clock"`
	Metadata    EventMetadata          `json:"metadata"`
}

// Actor represents the entity performing the action
type Actor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// EventMetadata contains system-level metadata
type EventMetadata struct {
	ReceivedAt    time.Time `json:"received_at"`
	ProcessedAt   time.Time `json:"processed_at"`
	BoundaryNode  string    `json:"boundary_node"`
	CorrelationID string    `json:"correlation_id"`
	RetryCount    int       `json:"retry_count"`
	SchemaVersion string    `json:"schema_version"`
}

// SubmitRequest represents a request to submit a certified event
type SubmitRequest struct {
	Event Event `json:"event"`
}

// SubmitResponse represents the response after submitting to ImmutableLedger
type SubmitResponse struct {
	Success         bool      `json:"success"`
	SequenceNumber  uint64    `json:"sequence_number"`
	EventID         string    `json:"event_id"`
	EventHash       string    `json:"event_hash"`
	PreviousHash    string    `json:"previous_hash"`
	SealedTimestamp time.Time `json:"sealed_timestamp"`
	CommitLatencyMS int64     `json:"commit_latency_ms"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
}
