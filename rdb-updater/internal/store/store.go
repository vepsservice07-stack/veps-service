package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/veps-service-480701/rdb-updater/pkg/models"
)

// Store handles PostgreSQL database operations
type Store struct {
	db *sql.DB
}

// Config holds database configuration
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
}

// New creates a new Store instance and connects to PostgreSQL
func New(config Config) (*Store, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		config.Host,
		config.Port,
		config.User,
		config.Password,
		config.Database,
		config.SSLMode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("[Store] Successfully connected to PostgreSQL")

	store := &Store{db: db}

	// Initialize schema
	if err := store.InitSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// InitSchema creates the necessary tables if they don't exist
func (s *Store) InitSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS events (
		id UUID PRIMARY KEY,
		type VARCHAR(255) NOT NULL,
		source VARCHAR(255) NOT NULL,
		timestamp TIMESTAMPTZ NOT NULL,
		actor_id VARCHAR(255) NOT NULL,
		actor_name VARCHAR(255) NOT NULL,
		actor_type VARCHAR(100),
		evidence JSONB NOT NULL,
		vector_clock JSONB NOT NULL,
		boundary_node VARCHAR(255),
		correlation_id UUID,
		received_at TIMESTAMPTZ NOT NULL,
		processed_at TIMESTAMPTZ,
		schema_version VARCHAR(50),
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_events_actor_id ON events(actor_id);
	CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
	CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
	CREATE INDEX IF NOT EXISTS idx_events_correlation_id ON events(correlation_id);
	CREATE INDEX IF NOT EXISTS idx_events_vector_clock ON events USING GIN(vector_clock);
	`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Println("[Store] Schema initialized successfully")
	return nil
}

// UpsertEvent inserts or updates an event in the database
func (s *Store) UpsertEvent(ctx context.Context, event models.Event) error {
	// Marshal JSONB fields
	evidenceJSON, err := json.Marshal(event.Evidence)
	if err != nil {
		return fmt.Errorf("failed to marshal evidence: %w", err)
	}

	vectorClockJSON, err := json.Marshal(event.VectorClock)
	if err != nil {
		return fmt.Errorf("failed to marshal vector clock: %w", err)
	}

	query := `
		INSERT INTO events (
			id, type, source, timestamp, 
			actor_id, actor_name, actor_type,
			evidence, vector_clock,
			boundary_node, correlation_id, 
			received_at, processed_at, schema_version
		) VALUES (
			$1, $2, $3, $4, 
			$5, $6, $7,
			$8, $9,
			$10, $11, 
			$12, $13, $14
		)
		ON CONFLICT (id) DO UPDATE SET
			type = EXCLUDED.type,
			source = EXCLUDED.source,
			timestamp = EXCLUDED.timestamp,
			actor_id = EXCLUDED.actor_id,
			actor_name = EXCLUDED.actor_name,
			actor_type = EXCLUDED.actor_type,
			evidence = EXCLUDED.evidence,
			vector_clock = EXCLUDED.vector_clock,
			boundary_node = EXCLUDED.boundary_node,
			correlation_id = EXCLUDED.correlation_id,
			processed_at = EXCLUDED.processed_at
	`

	processedAt := time.Now().UTC()
	if !event.Metadata.ProcessedAt.IsZero() {
		processedAt = event.Metadata.ProcessedAt
	}

	_, err = s.db.ExecContext(
		ctx,
		query,
		event.ID,
		event.Type,
		event.Source,
		event.Timestamp,
		event.Actor.ID,
		event.Actor.Name,
		event.Actor.Type,
		evidenceJSON,
		vectorClockJSON,
		event.Metadata.BoundaryNode,
		event.Metadata.CorrelationID,
		event.Metadata.ReceivedAt,
		processedAt,
		event.Metadata.SchemaVersion,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert event: %w", err)
	}

	log.Printf("[Store] Event %s upserted successfully", event.ID)
	return nil
}

// GetEventByID retrieves an event by its ID
func (s *Store) GetEventByID(ctx context.Context, id string) (*models.Event, error) {
	query := `
		SELECT id, type, source, timestamp,
			actor_id, actor_name, actor_type,
			evidence, vector_clock,
			boundary_node, correlation_id,
			received_at, processed_at, schema_version
		FROM events
		WHERE id = $1
	`

	var event models.Event
	var evidenceJSON, vectorClockJSON []byte
	var processedAt sql.NullTime
	var actorType, boundaryNode, schemaVersion sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&event.ID,
		&event.Type,
		&event.Source,
		&event.Timestamp,
		&event.Actor.ID,
		&event.Actor.Name,
		&actorType,
		&evidenceJSON,
		&vectorClockJSON,
		&boundaryNode,
		&event.Metadata.CorrelationID,
		&event.Metadata.ReceivedAt,
		&processedAt,
		&schemaVersion,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query event: %w", err)
	}

	// Unmarshal JSONB fields
	if err := json.Unmarshal(evidenceJSON, &event.Evidence); err != nil {
		return nil, fmt.Errorf("failed to unmarshal evidence: %w", err)
	}

	if err := json.Unmarshal(vectorClockJSON, &event.VectorClock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vector clock: %w", err)
	}

	// Handle nullable fields
	if actorType.Valid {
		event.Actor.Type = actorType.String
	}
	if boundaryNode.Valid {
		event.Metadata.BoundaryNode = boundaryNode.String
	}
	if processedAt.Valid {
		event.Metadata.ProcessedAt = processedAt.Time
	}
	if schemaVersion.Valid {
		event.Metadata.SchemaVersion = schemaVersion.String
	}

	return &event, nil
}

// CheckVectorClockCausality checks if all events in the vector clock exist
// Returns true if all causal dependencies are satisfied
func (s *Store) CheckVectorClockCausality(ctx context.Context, vc models.VectorClock) (bool, []string, error) {
	missing := []string{}

	for nodeID, timestamp := range vc {
		query := `
			SELECT COUNT(*) 
			FROM events 
			WHERE boundary_node = $1 
			AND (vector_clock->>$1)::bigint <= $2
		`

		var count int
		err := s.db.QueryRowContext(ctx, query, nodeID, timestamp).Scan(&count)
		if err != nil {
			return false, nil, fmt.Errorf("failed to check causality: %w", err)
		}

		if count == 0 {
			missing = append(missing, nodeID)
		}
	}

	return len(missing) == 0, missing, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// HealthCheck verifies database connectivity
func (s *Store) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}