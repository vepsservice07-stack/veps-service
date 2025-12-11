package models

import (
	"time"
)

// ClientEventRequest represents the client's event submission format
type ClientEventRequest struct {
	EventType        string                 `json:"event_type"`
	NoteID           int                    `json:"note_id,omitempty"`
	UserID           string                 `json:"user_id"`
	BPM              int                    `json:"bpm,omitempty"`
	DurationMS       int                    `json:"duration_ms,omitempty"`
	TimestampClient  int64                  `json:"timestamp_client"` // ms since epoch
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// ClientEventResponse represents the response after event submission
type ClientEventResponse struct {
	SequenceNumber uint64                 `json:"sequence_number"`
	VectorClock    map[string]int64       `json:"vector_clock"`
	ProofHash      string                 `json:"proof_hash"`
	TimestampVEPS  int64                  `json:"timestamp_veps"` // ms since epoch
	EventID        string                 `json:"event_id"`
}

// CausalityRequest represents a causality check request
type CausalityRequest struct {
	EventA uint64 `json:"event_a"` // sequence number
	EventB uint64 `json:"event_b"` // sequence number
}

// CausalityResponse represents the result of a causality check
type CausalityResponse struct {
	Relationship string  `json:"relationship"` // "happened-before", "happened-after", "concurrent"
	TimeDeltaMS  int64   `json:"time_delta_ms"`
	Confidence   float64 `json:"confidence"`
}

// BatchQueryRequest represents a batch event retrieval request
type BatchQueryRequest struct {
	NoteID       *int   `json:"note_id,omitempty"`
	UserID       *string `json:"user_id,omitempty"`
	StartSeq     *uint64 `json:"start_seq,omitempty"`
	EndSeq       *uint64 `json:"end_seq,omitempty"`
	StartTime    *int64  `json:"start_time,omitempty"` // ms since epoch
	EndTime      *int64  `json:"end_time,omitempty"`   // ms since epoch
	Limit        int     `json:"limit,omitempty"`
}

// BatchQueryResponse represents the batch retrieval response
type BatchQueryResponse struct {
	Events     []EventSummary `json:"events"`
	TotalCount int            `json:"total_count"`
}

// EventSummary represents a summary of a single event
type EventSummary struct {
	SequenceNumber uint64                 `json:"sequence_number"`
	EventType      string                 `json:"event_type"`
	TimestampVEPS  int64                  `json:"timestamp_veps"` // ms since epoch
	NoteID         *int                   `json:"note_id,omitempty"`
	UserID         string                 `json:"user_id,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// BoundaryEvent represents the format expected by Boundary Adapter
type BoundaryEvent struct {
	Source string                 `json:"source"`
	Data   map[string]interface{} `json:"data"`
}

// BoundaryResponse represents the response from Boundary Adapter
type BoundaryResponse struct {
	Success     bool              `json:"success"`
	Message     string            `json:"message,omitempty"`
	EventID     string            `json:"event_id,omitempty"`
	Timestamp   time.Time         `json:"timestamp,omitempty"`
	VectorClock map[string]int64  `json:"vector_clock,omitempty"`
	Duration    string            `json:"duration,omitempty"`
}

// StandardResponse represents the standard API response format
type StandardResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}
