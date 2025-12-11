package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/veps-service-480701/monolith-submitter/internal/client"
	"github.com/veps-service-480701/monolith-submitter/pkg/models"
)

// Handler manages HTTP requests for the Monolith Submitter
type Handler struct {
	ledgerClient *client.LedgerClient
}

// New creates a new HTTP handler
func New(ledgerClient *client.LedgerClient) *Handler {
	return &Handler{
		ledgerClient: ledgerClient,
	}
}

// Response represents the standard API response format
type Response struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Duration  string      `json:"duration,omitempty"`
}

// HealthCheck handles health check requests
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Check ImmutableLedger health
	healthy, status, lastSeq, err := h.ledgerClient.HealthCheck(ctx)

	if err != nil || !healthy {
		response := Response{
			Success:   false,
			Message:   "Monolith Submitter is unhealthy",
			Error:     fmt.Sprintf("Ledger health check failed: %v", err),
			Timestamp: time.Now().UTC(),
			Data: map[string]interface{}{
				"ledger_healthy": healthy,
				"ledger_status":  status,
			},
		}
		h.writeJSON(w, http.StatusServiceUnavailable, response)
		return
	}

	response := Response{
		Success:   true,
		Message:   "Monolith Submitter is healthy",
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"ledger_healthy":          healthy,
			"ledger_status":           status,
			"ledger_last_sequence":    lastSeq,
			"submitter_ready":         true,
		},
	}
	h.writeJSON(w, http.StatusOK, response)
}

// SubmitEvent handles certified event submission to ImmutableLedger
func (h *Handler) SubmitEvent(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse submit request
	var submitReq models.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&submitReq); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Validate event
	if submitReq.Event.ID.String() == "" {
		h.writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	if submitReq.Event.Type == "" {
		h.writeError(w, http.StatusBadRequest, "event type is required")
		return
	}

	log.Printf("[Handler] Submitting event %s to ImmutableLedger", submitReq.Event.ID)

	// Submit to ImmutableLedger with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	submitResp, err := h.ledgerClient.SubmitEvent(ctx, submitReq.Event)
	if err != nil {
		log.Printf("[Handler] Failed to submit event %s: %v", submitReq.Event.ID, err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("ledger submission failed: %v", err))
		return
	}

	duration := time.Since(startTime)

	log.Printf("[Handler] Event %s sealed with sequence %d in %s",
		submitReq.Event.ID, submitResp.SequenceNumber, duration)

	// Return success response
	response := Response{
		Success:   true,
		Message:   "Event submitted and sealed successfully",
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data:      submitResp,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetEvent handles retrieval of sealed events by sequence number
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Parse sequence number from query params
	seqStr := r.URL.Query().Get("sequence")
	if seqStr == "" {
		h.writeError(w, http.StatusBadRequest, "sequence parameter is required")
		return
	}

	var sequence uint64
	if _, err := fmt.Sscanf(seqStr, "%d", &sequence); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid sequence number")
		return
	}

	log.Printf("[Handler] Retrieving event with sequence %d", sequence)

	// Get from ImmutableLedger
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sealedEvent, err := h.ledgerClient.GetEvent(ctx, sequence)
	if err != nil {
		log.Printf("[Handler] Failed to get event %d: %v", sequence, err)
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("event not found: %v", err))
		return
	}

	// Parse payload back to JSON
	var eventPayload map[string]interface{}
	if err := json.Unmarshal(sealedEvent.Payload, &eventPayload); err != nil {
		eventPayload = map[string]interface{}{"raw_bytes": string(sealedEvent.Payload)}
	}

	response := Response{
		Success:   true,
		Message:   "Event retrieved successfully",
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"sequence_number":  sealedEvent.SequenceNumber,
			"event_id":         sealedEvent.EventId,
			"event_hash":       sealedEvent.EventHash,
			"previous_hash":    sealedEvent.PreviousHash,
			"sealed_timestamp": time.UnixMilli(sealedEvent.SealedTimestamp),
			"commit_latency_ms": sealedEvent.CommitLatencyMs,
			"payload":          eventPayload,
		},
	}

	h.writeJSON(w, http.StatusOK, response)
}

// writeJSON writes a JSON response
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[Handler] Error encoding JSON response: %v", err)
	}
}

// writeError writes an error response
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	response := Response{
		Success:   false,
		Error:     message,
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, status, response)
}

// RegisterRoutes sets up HTTP routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.HealthCheck)
	mux.HandleFunc("/submit", h.SubmitEvent)
	mux.HandleFunc("/event", h.GetEvent)
}
