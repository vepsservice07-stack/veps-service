package client

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/veps-service-480701/monolith-submitter/pkg/ledger"
	"github.com/veps-service-480701/monolith-submitter/pkg/models"
)

// LedgerClient handles communication with ImmutableLedger
type LedgerClient struct {
	conn      *grpc.ClientConn
	client    pb.ImmutableLedgerClient
	secretKey []byte
	nodeID    string
}

// NewLedgerClient creates a new ImmutableLedger client
func NewLedgerClient(address string, secretKey string, nodeID string) (*LedgerClient, error) {
	// Create gRPC connection
	conn, err := grpc.Dial(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(10*1024*1024), // 10MB
			grpc.MaxCallSendMsgSize(10*1024*1024), // 10MB
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ledger: %w", err)
	}

	client := pb.NewImmutableLedgerClient(conn)

	log.Printf("[LedgerClient] Connected to ImmutableLedger at %s", address)

	return &LedgerClient{
		conn:      conn,
		client:    client,
		secretKey: []byte(secretKey),
		nodeID:    nodeID,
	}, nil
}

// Close closes the gRPC connection
func (lc *LedgerClient) Close() error {
	return lc.conn.Close()
}

// SubmitEvent submits a certified event to the ImmutableLedger
func (lc *LedgerClient) SubmitEvent(ctx context.Context, event models.Event) (*models.SubmitResponse, error) {
	startTime := time.Now()

	// Serialize event to JSON
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	// Generate cryptographic signature
	signature := lc.signEvent(payload)

	// Create certified event
	certifiedEvent := &pb.CertifiedEvent{
		EventId:       event.ID.String(),
		Payload:       payload,
		VepsSignature: signature,
		VepsTimestamp: time.Now().UnixMilli(),
		Metadata: map[string]string{
			"veps_node":       lc.nodeID,
			"boundary_node":   event.Metadata.BoundaryNode,
			"correlation_id":  event.Metadata.CorrelationID,
			"event_type":      event.Type,
			"event_source":    event.Source,
			"actor_id":        event.Actor.ID,
			"original_timestamp": event.Timestamp.Format(time.RFC3339Nano),
		},
	}

	log.Printf("[LedgerClient] Submitting event %s to ImmutableLedger", event.ID)

	// Call gRPC
	sealedEvent, err := lc.client.SubmitEvent(ctx, certifiedEvent)
	if err != nil {
		return nil, fmt.Errorf("ledger submission failed: %w", err)
	}

	duration := time.Since(startTime)

	log.Printf("[LedgerClient] Event %s sealed with sequence number %d (latency: %s)",
		event.ID, sealedEvent.SequenceNumber, duration)

	// Build response
	response := &models.SubmitResponse{
		Success:         true,
		SequenceNumber:  sealedEvent.SequenceNumber,
		EventID:         sealedEvent.EventId,
		EventHash:       sealedEvent.EventHash,
		PreviousHash:    sealedEvent.PreviousHash,
		SealedTimestamp: time.UnixMilli(sealedEvent.SealedTimestamp),
		CommitLatencyMS: sealedEvent.CommitLatencyMs,
		Message:         fmt.Sprintf("Event sealed successfully in %s", duration),
	}

	return response, nil
}

// signEvent creates an HMAC-SHA256 signature for the event
func (lc *LedgerClient) signEvent(payload []byte) string {
	mac := hmac.New(sha256.New, lc.secretKey)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// HealthCheck checks if the ImmutableLedger is healthy
func (lc *LedgerClient) HealthCheck(ctx context.Context) (bool, string, uint64, error) {
	// Create a context with 15-second timeout for initial connection
	healthCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req := &pb.HealthCheckRequest{}

	resp, err := lc.client.HealthCheck(healthCtx, req)
	if err != nil {
		return false, "", 0, fmt.Errorf("health check failed: %w", err)
	}

	return resp.Healthy, resp.Status, resp.LastSequenceNumber, nil
}

// GetEvent retrieves a sealed event by sequence number
func (lc *LedgerClient) GetEvent(ctx context.Context, sequenceNumber uint64) (*pb.SealedEvent, error) {
	req := &pb.GetEventRequest{
		SequenceNumber: sequenceNumber,
	}

	resp, err := lc.client.GetEvent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	return resp, nil
}
