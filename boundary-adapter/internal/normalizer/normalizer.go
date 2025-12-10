package normalizer

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/veps-service-480701/boundary-adapter/pkg/models"
)

// Normalizer handles the transformation of raw events into canonical Event format
type Normalizer struct {
	nodeID string // This VEPS instance's ID for vector clock
}

// New creates a new Normalizer instance
func New() *Normalizer {
	// Get node ID from environment or generate one
	nodeID := os.Getenv("VEPS_NODE_ID")
	if nodeID == "" {
		nodeID = uuid.New().String()
	}

	return &Normalizer{
		nodeID: nodeID,
	}
}

// Normalize transforms a RawEvent into a canonical Event with vector clock
func (n *Normalizer) Normalize(raw models.RawEvent) (*models.Event, error) {
	// Validate required fields
	if raw.Source == "" {
		return nil, fmt.Errorf("source is required")
	}

	if raw.Data == nil {
		return nil, fmt.Errorf("data is required")
	}

	// Extract event type from data
	eventType, ok := raw.Data["type"].(string)
	if !ok || eventType == "" {
		return nil, fmt.Errorf("event type is required in data")
	}

	// Extract actor information
	actor, err := n.extractActor(raw.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to extract actor: %w", err)
	}

	// Determine timestamp - use provided or current time
	timestamp := raw.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	// Initialize vector clock for this event
	vectorClock := models.VectorClock{
		n.nodeID: time.Now().UnixNano(), // Use nanosecond timestamp as logical clock
	}

	// If incoming event has a vector clock, merge it
	if incomingVC, ok := raw.Data["vector_clock"].(map[string]interface{}); ok {
		existingVC := n.parseVectorClock(incomingVC)
		vectorClock.Merge(existingVC)
	}
	vectorClock.Increment(n.nodeID)

	// Create the normalized event
	event := &models.Event{
		ID:          uuid.New(),
		Type:        eventType,
		Source:      raw.Source,
		Timestamp:   timestamp,
		Actor:       actor,
		Evidence:    n.extractEvidence(raw.Data),
		VectorClock: vectorClock,
		Metadata: models.EventMetadata{
			ReceivedAt:    time.Now().UTC(),
			BoundaryNode:  n.nodeID,
			CorrelationID: n.extractCorrelationID(raw.Data),
			SchemaVersion: "1.0",
		},
	}

	return event, nil
}

// extractActor pulls actor information from raw data
func (n *Normalizer) extractActor(data map[string]any) (models.Actor, error) {
	actor := models.Actor{
		Metadata: make(map[string]string),
	}

	// Try to extract actor from nested object
	if actorData, ok := data["actor"].(map[string]interface{}); ok {
		if id, ok := actorData["id"].(string); ok {
			actor.ID = id
		}
		if name, ok := actorData["name"].(string); ok {
			actor.Name = name
		}
		if actorType, ok := actorData["type"].(string); ok {
			actor.Type = actorType
		}
	} else {
		// Fallback: try top-level fields
		if userID, ok := data["user_id"].(string); ok {
			actor.ID = userID
		} else if actorID, ok := data["actor_id"].(string); ok {
			actor.ID = actorID
		}

		if userName, ok := data["user_name"].(string); ok {
			actor.Name = userName
		} else if actorName, ok := data["actor_name"].(string); ok {
			actor.Name = actorName
		}
	}

	// Actor ID is required
	if actor.ID == "" {
		return actor, fmt.Errorf("actor ID is required")
	}

	// Default name to ID if not provided
	if actor.Name == "" {
		actor.Name = actor.ID
	}

	// Default type if not provided
	if actor.Type == "" {
		actor.Type = "user"
	}

	return actor, nil
}

// extractEvidence creates the evidence payload (everything except metadata fields)
func (n *Normalizer) extractEvidence(data map[string]any) map[string]any {
	evidence := make(map[string]any)

	// Copy all data except internal fields
	excludeFields := map[string]bool{
		"type":         true,
		"actor":        true,
		"user_id":      true,
		"user_name":    true,
		"actor_id":     true,
		"actor_name":   true,
		"vector_clock": true,
		"correlation_id": true,
	}

	for key, value := range data {
		if !excludeFields[key] {
			evidence[key] = value
		}
	}

	return evidence
}

// extractCorrelationID gets or generates a correlation ID for tracing
func (n *Normalizer) extractCorrelationID(data map[string]any) string {
	if corrID, ok := data["correlation_id"].(string); ok && corrID != "" {
		return corrID
	}
	return uuid.New().String()
}

// parseVectorClock converts a raw map to VectorClock
func (n *Normalizer) parseVectorClock(raw map[string]interface{}) models.VectorClock {
	vc := make(models.VectorClock)
	
	for nodeID, value := range raw {
		switch v := value.(type) {
		case float64:
			vc[nodeID] = int64(v)
		case int64:
			vc[nodeID] = v
		case int:
			vc[nodeID] = int64(v)
		}
	}
	
	return vc
}

// ValidateSchema performs basic schema validation on raw events
func (n *Normalizer) ValidateSchema(raw models.RawEvent) error {
	if raw.Source == "" {
		return fmt.Errorf("source field is required")
	}

	if raw.Data == nil {
		return fmt.Errorf("data field is required")
	}

	if _, ok := raw.Data["type"].(string); !ok {
		return fmt.Errorf("type field is required in data")
	}

	// Validate actor can be extracted
	if _, err := n.extractActor(raw.Data); err != nil {
		return fmt.Errorf("invalid actor data: %w", err)
	}

	return nil
}