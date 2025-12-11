package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/veps-service-480701/api-gateway/pkg/models"
)

// Client handles database operations
type Client struct {
	db *sql.DB
}

// NewClient creates a new database client
func NewClient(connectionString string) (*Client, error) {
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("[DB] Connected to PostgreSQL")

	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	return c.db.Close()
}

// GetEventBySequence retrieves an event by its sequence number
func (c *Client) GetEventBySequence(ctx context.Context, sequenceNumber uint64) (*models.EventSummary, error) {
	query := `
		SELECT 
			id, 
			type, 
			source, 
			timestamp, 
			actor, 
			evidence, 
			vector_clock, 
			metadata
		FROM events
		WHERE id = $1
	`

	var (
		id          string
		eventType   string
		source      string
		timestamp   time.Time
		actorJSON   []byte
		evidenceJSON []byte
		vectorClockJSON []byte
		metadataJSON []byte
	)

	err := c.db.QueryRowContext(ctx, query, sequenceNumber).Scan(
		&id,
		&eventType,
		&source,
		&timestamp,
		&actorJSON,
		&evidenceJSON,
		&vectorClockJSON,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query event: %w", err)
	}

	// Parse metadata to extract note_id and user_id
	var metadata map[string]interface{}
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		log.Printf("[DB] Warning: failed to parse metadata: %v", err)
		metadata = make(map[string]interface{})
	}

	var evidence map[string]interface{}
	if err := json.Unmarshal(evidenceJSON, &evidence); err != nil {
		log.Printf("[DB] Warning: failed to parse evidence: %v", err)
		evidence = make(map[string]interface{})
	}

	var actor map[string]interface{}
	if err := json.Unmarshal(actorJSON, &actor); err != nil {
		log.Printf("[DB] Warning: failed to parse actor: %v", err)
		actor = make(map[string]interface{})
	}

	// Extract note_id and user_id
	var noteID *int
	if nid, ok := evidence["note_id"].(float64); ok {
		val := int(nid)
		noteID = &val
	}

	userID := ""
	if uid, ok := actor["id"].(string); ok {
		userID = uid
	}

	return &models.EventSummary{
		SequenceNumber: sequenceNumber,
		EventType:      eventType,
		TimestampVEPS:  timestamp.UnixMilli(),
		NoteID:         noteID,
		UserID:         userID,
		Metadata:       metadata,
	}, nil
}

// BatchQuery retrieves events based on filters
func (c *Client) BatchQuery(ctx context.Context, req models.BatchQueryRequest) ([]models.EventSummary, int, error) {
	// Build dynamic query
	query := `
		SELECT 
			id,
			type,
			timestamp,
			actor,
			evidence,
			metadata
		FROM events
		WHERE 1=1
	`

	args := []interface{}{}
	argCount := 1

	// Add filters
	if req.NoteID != nil {
		query += fmt.Sprintf(" AND evidence->>'note_id' = $%d", argCount)
		args = append(args, fmt.Sprintf("%d", *req.NoteID))
		argCount++
	}

	if req.UserID != nil {
		query += fmt.Sprintf(" AND actor->>'id' = $%d", argCount)
		args = append(args, *req.UserID)
		argCount++
	}

	if req.StartTime != nil {
		query += fmt.Sprintf(" AND timestamp >= $%d", argCount)
		args = append(args, time.UnixMilli(*req.StartTime))
		argCount++
	}

	if req.EndTime != nil {
		query += fmt.Sprintf(" AND timestamp <= $%d", argCount)
		args = append(args, time.UnixMilli(*req.EndTime))
		argCount++
	}

	// Order by timestamp
	query += " ORDER BY timestamp DESC"

	// Apply limit
	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100 // Default limit
	}
	query += fmt.Sprintf(" LIMIT $%d", argCount)
	args = append(args, limit)

	// Execute query
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Parse results
	events := []models.EventSummary{}
	for rows.Next() {
		var (
			id           string
			eventType    string
			timestamp    time.Time
			actorJSON    []byte
			evidenceJSON []byte
			metadataJSON []byte
		)

		if err := rows.Scan(&id, &eventType, &timestamp, &actorJSON, &evidenceJSON, &metadataJSON); err != nil {
			log.Printf("[DB] Warning: failed to scan row: %v", err)
			continue
		}

		var evidence map[string]interface{}
		json.Unmarshal(evidenceJSON, &evidence)

		var actor map[string]interface{}
		json.Unmarshal(actorJSON, &actor)

		var metadata map[string]interface{}
		json.Unmarshal(metadataJSON, &metadata)

		// Extract note_id and user_id
		var noteID *int
		if nid, ok := evidence["note_id"].(float64); ok {
			val := int(nid)
			noteID = &val
		}

		userID := ""
		if uid, ok := actor["id"].(string); ok {
			userID = uid
		}

		// Parse sequence number from ID (assuming it's stored as string)
		var seqNum uint64
		fmt.Sscanf(id, "%d", &seqNum)

		events = append(events, models.EventSummary{
			SequenceNumber: seqNum,
			EventType:      eventType,
			TimestampVEPS:  timestamp.UnixMilli(),
			NoteID:         noteID,
			UserID:         userID,
			Metadata:       metadata,
		})
	}

	return events, len(events), nil
}

// CompareCausality compares two events by sequence number
func (c *Client) CompareCausality(ctx context.Context, seqA, seqB uint64) (string, int64, error) {
	// Query both events
	eventA, err := c.GetEventBySequence(ctx, seqA)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get event A: %w", err)
	}

	eventB, err := c.GetEventBySequence(ctx, seqB)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get event B: %w", err)
	}

	// Compare sequence numbers (total order)
	relationship := ""
	if seqA < seqB {
		relationship = "happened-before"
	} else if seqA > seqB {
		relationship = "happened-after"
	} else {
		relationship = "concurrent" // Same sequence (shouldn't happen with ImmutableLedger)
	}

	// Calculate time delta
	timeDelta := eventB.TimestampVEPS - eventA.TimestampVEPS

	return relationship, timeDelta, nil
}
