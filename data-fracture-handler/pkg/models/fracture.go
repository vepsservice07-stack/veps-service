package models

import (
	"time"

	"github.com/google/uuid"
)

// FracturedEvent represents a vetoed event stored for audit
type FracturedEvent struct {
	FractureID uuid.UUID        `json:"fracture_id"`
	Timestamp  time.Time        `json:"timestamp"`
	Event      Event            `json:"event"`
	Rejection  RejectionDetails `json:"rejection"`
	Context    FractureContext  `json:"context"`
}

// RejectionDetails contains information about why the event was vetoed
type RejectionDetails struct {
	FailedChecks []string `json:"failed_checks"`
	Reasons      []string `json:"reasons"`
	VetoNode     string   `json:"veto_service_node"`
	Duration     string   `json:"validation_duration,omitempty"`
}

// FractureContext contains contextual information for investigation
type FractureContext struct {
	VEPSNodeID     string                 `json:"veps_node_id"`
	CorrelationID  string                 `json:"correlation_id"`
	VectorClock    map[string]int64       `json:"vector_clock"`
	OriginalSource string                 `json:"original_source"`
	ReceivedAt     time.Time              `json:"received_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Event represents the normalized event that was rejected
// This mirrors the structure from VEPS
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

// FractureRequest represents the incoming request to log a vetoed event
type FractureRequest struct {
	Event         Event            `json:"event"`
	FailedChecks  []string         `json:"failed_checks"`
	Reasons       []string         `json:"reasons"`
	VetoNode      string           `json:"veto_node"`
	Duration      string           `json:"duration,omitempty"`
	CorrelationID string           `json:"correlation_id,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ToFracturedEvent converts a FractureRequest to a FracturedEvent
func (fr *FractureRequest) ToFracturedEvent() *FracturedEvent {
	return &FracturedEvent{
		FractureID: uuid.New(),
		Timestamp:  time.Now().UTC(),
		Event:      fr.Event,
		Rejection: RejectionDetails{
			FailedChecks: fr.FailedChecks,
			Reasons:      fr.Reasons,
			VetoNode:     fr.VetoNode,
			Duration:     fr.Duration,
		},
		Context: FractureContext{
			VEPSNodeID:     fr.Event.Metadata.BoundaryNode,
			CorrelationID:  fr.CorrelationID,
			VectorClock:    fr.Event.VectorClock,
			OriginalSource: fr.Event.Source,
			ReceivedAt:     fr.Event.Metadata.ReceivedAt,
			Metadata:       fr.Metadata,
		},
	}
}
