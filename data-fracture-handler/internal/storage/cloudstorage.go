package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"cloud.google.com/go/storage"
	"github.com/veps-service-480701/data-fracture-handler/pkg/models"
	"google.golang.org/api/iterator"
)

// CloudStorageWriter handles writing fractured events to Google Cloud Storage
type CloudStorageWriter struct {
	client     *storage.Client
	bucketName string
}

// NewCloudStorageWriter creates a new Cloud Storage writer
func NewCloudStorageWriter(ctx context.Context, bucketName string) (*CloudStorageWriter, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	return &CloudStorageWriter{
		client:     client,
		bucketName: bucketName,
	}, nil
}

// WriteFracture writes a fractured event to Cloud Storage
// Organizes by date: gs://bucket/YYYY/MM/DD/fractures-HH.jsonl
func (w *CloudStorageWriter) WriteFracture(ctx context.Context, fracture *models.FracturedEvent) error {
	// Generate object path based on timestamp
	objectPath := w.generateObjectPath(fracture.Timestamp)

	// Get bucket and object
	bucket := w.client.Bucket(w.bucketName)
	obj := bucket.Object(objectPath)

	// Open writer in append mode (for JSONL format)
	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/x-ndjson" // Newline-delimited JSON

	// Serialize fracture to JSON
	jsonData, err := json.Marshal(fracture)
	if err != nil {
		return fmt.Errorf("failed to marshal fracture: %w", err)
	}

	// Write JSON line (with newline for JSONL format)
	jsonLine := append(jsonData, '\n')
	if _, err := writer.Write(jsonLine); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write to Cloud Storage: %w", err)
	}

	// Close writer (finalizes the upload)
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close Cloud Storage writer: %w", err)
	}

	log.Printf("[CloudStorage] Fracture %s written to gs://%s/%s", 
		fracture.FractureID, w.bucketName, objectPath)

	return nil
}

// WriteFractureBatch writes multiple fractured events efficiently
func (w *CloudStorageWriter) WriteFractureBatch(ctx context.Context, fractures []*models.FracturedEvent) error {
	if len(fractures) == 0 {
		return nil
	}

	// Group by hour for efficient batching
	batches := w.groupByHour(fractures)

	for objectPath, batch := range batches {
		if err := w.writeBatch(ctx, objectPath, batch); err != nil {
			return err
		}
	}

	return nil
}

// writeBatch writes a batch of fractures to a single file
func (w *CloudStorageWriter) writeBatch(ctx context.Context, objectPath string, fractures []*models.FracturedEvent) error {
	bucket := w.client.Bucket(w.bucketName)
	obj := bucket.Object(objectPath)

	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/x-ndjson"

	for _, fracture := range fractures {
		jsonData, err := json.Marshal(fracture)
		if err != nil {
			writer.Close()
			return fmt.Errorf("failed to marshal fracture: %w", err)
		}

		jsonLine := append(jsonData, '\n')
		if _, err := writer.Write(jsonLine); err != nil {
			writer.Close()
			return fmt.Errorf("failed to write to Cloud Storage: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close Cloud Storage writer: %w", err)
	}

	log.Printf("[CloudStorage] Batch of %d fractures written to gs://%s/%s", 
		len(fractures), w.bucketName, objectPath)

	return nil
}

// ReadFractures reads fractured events from Cloud Storage (for queries/debugging)
func (w *CloudStorageWriter) ReadFractures(ctx context.Context, date time.Time) ([]*models.FracturedEvent, error) {
	// List all objects for the given date
	prefix := w.generateDatePrefix(date)
	
	bucket := w.client.Bucket(w.bucketName)
	query := &storage.Query{Prefix: prefix}
	
	it := bucket.Objects(ctx, query)
	
	var allFractures []*models.FracturedEvent
	
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate objects: %w", err)
		}

		// Read object
		fractures, err := w.readObject(ctx, attrs.Name)
		if err != nil {
			log.Printf("[CloudStorage] Warning: failed to read %s: %v", attrs.Name, err)
			continue
		}

		allFractures = append(allFractures, fractures...)
	}

	return allFractures, nil
}

// readObject reads a single JSONL file and parses fractures
func (w *CloudStorageWriter) readObject(ctx context.Context, objectPath string) ([]*models.FracturedEvent, error) {
	bucket := w.client.Bucket(w.bucketName)
	obj := bucket.Object(objectPath)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}
	defer reader.Close()

	var fractures []*models.FracturedEvent
	decoder := json.NewDecoder(reader)

	for {
		var fracture models.FracturedEvent
		if err := decoder.Decode(&fracture); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode fracture: %w", err)
		}
		fractures = append(fractures, &fracture)
	}

	return fractures, nil
}

// generateObjectPath creates the GCS path for a fracture
// Format: YYYY/MM/DD/fractures-HH.jsonl
func (w *CloudStorageWriter) generateObjectPath(timestamp time.Time) string {
	return fmt.Sprintf("%04d/%02d/%02d/fractures-%02d.jsonl",
		timestamp.Year(),
		timestamp.Month(),
		timestamp.Day(),
		timestamp.Hour(),
	)
}

// generateDatePrefix creates the prefix for listing objects by date
func (w *CloudStorageWriter) generateDatePrefix(date time.Time) string {
	return fmt.Sprintf("%04d/%02d/%02d/",
		date.Year(),
		date.Month(),
		date.Day(),
	)
}

// groupByHour groups fractures by hour for efficient batching
func (w *CloudStorageWriter) groupByHour(fractures []*models.FracturedEvent) map[string][]*models.FracturedEvent {
	batches := make(map[string][]*models.FracturedEvent)

	for _, fracture := range fractures {
		path := w.generateObjectPath(fracture.Timestamp)
		batches[path] = append(batches[path], fracture)
	}

	return batches
}

// Close closes the Cloud Storage client
func (w *CloudStorageWriter) Close() error {
	return w.client.Close()
}
