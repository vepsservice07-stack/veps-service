package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/veps-service-480701/api-gateway/internal/database"
	"github.com/veps-service-480701/api-gateway/pkg/models"
)

// Handler manages API Gateway HTTP requests
type Handler struct {
	boundaryURL string
	dbClient    *database.Client
}

// New creates a new API Gateway handler
func New(boundaryURL string, dbClient *database.Client) *Handler {
	return &Handler{
		boundaryURL: boundaryURL,
		dbClient:    dbClient,
	}
}

// SubmitEvent handles POST /api/v1/events
func (h *Handler) SubmitEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse client request
	var clientReq models.ClientEventRequest
	if err := json.NewDecoder(r.Body).Decode(&clientReq); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Validate required fields
	if clientReq.EventType == "" {
		h.writeError(w, http.StatusBadRequest, "event_type is required")
		return
	}
	if clientReq.UserID == "" {
		h.writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	log.Printf("[Gateway] Submitting event: type=%s, user=%s, note=%d", 
		clientReq.EventType, clientReq.UserID, clientReq.NoteID)

	// Transform to VEPS format (Boundary Adapter format)
	boundaryEvent := models.BoundaryEvent{
		Source: "second-brain",
		Data: map[string]interface{}{
			"type": clientReq.EventType,
			"actor": map[string]interface{}{
				"id":   clientReq.UserID,
				"name": clientReq.UserID,
				"type": "user",
			},
			"evidence": map[string]interface{}{
				"note_id":          clientReq.NoteID,
				"bpm":              clientReq.BPM,
				"duration_ms":      clientReq.DurationMS,
				"timestamp_client": clientReq.TimestampClient,
				"metadata":         clientReq.Metadata,
			},
		},
	}

	// Call Boundary Adapter
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	boundaryResp, err := h.callBoundaryAdapter(ctx, boundaryEvent)
	if err != nil {
		log.Printf("[Gateway] Failed to call Boundary Adapter: %v", err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to process event: %v", err))
		return
	}

	if !boundaryResp.Success {
		h.writeError(w, http.StatusInternalServerError, boundaryResp.Message)
		return
	}

	// Build client response
	// Note: sequence_number will be 0 until we integrate with Monolith Submitter
	// For now, using vector clock counter as sequence
	sequenceNumber := uint64(0)
	for _, counter := range boundaryResp.VectorClock {
		if uint64(counter) > sequenceNumber {
			sequenceNumber = uint64(counter)
		}
	}

	clientResp := models.ClientEventResponse{
		SequenceNumber: sequenceNumber,
		VectorClock:    boundaryResp.VectorClock,
		ProofHash:      "", // TODO: Add when Monolith Submitter integration complete
		TimestampVEPS:  boundaryResp.Timestamp.UnixMilli(),
		EventID:        boundaryResp.EventID,
	}

	log.Printf("[Gateway] Event submitted successfully: id=%s, seq=%d", 
		clientResp.EventID, clientResp.SequenceNumber)

	h.writeJSON(w, http.StatusOK, models.StandardResponse{
		Success:   true,
		Message:   "Event submitted successfully",
		Data:      clientResp,
		Timestamp: time.Now().UTC(),
	})
}

// CheckCausality handles GET /api/v1/causality
func (h *Handler) CheckCausality(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Parse query parameters
	eventAStr := r.URL.Query().Get("event_a")
	eventBStr := r.URL.Query().Get("event_b")

	if eventAStr == "" || eventBStr == "" {
		h.writeError(w, http.StatusBadRequest, "event_a and event_b parameters are required")
		return
	}

	eventA, err := strconv.ParseUint(eventAStr, 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "event_a must be a valid sequence number")
		return
	}

	eventB, err := strconv.ParseUint(eventBStr, 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "event_b must be a valid sequence number")
		return
	}

	log.Printf("[Gateway] Checking causality: %d vs %d", eventA, eventB)

	// Query database
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	relationship, timeDelta, err := h.dbClient.CompareCausality(ctx, eventA, eventB)
	if err != nil {
		log.Printf("[Gateway] Failed to check causality: %v", err)
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("failed to check causality: %v", err))
		return
	}

	// Build response
	causalityResp := models.CausalityResponse{
		Relationship: relationship,
		TimeDeltaMS:  timeDelta,
		Confidence:   1.0, // Always 1.0 with total ordering from ImmutableLedger
	}

	log.Printf("[Gateway] Causality result: %s (delta: %dms)", relationship, timeDelta)

	h.writeJSON(w, http.StatusOK, models.StandardResponse{
		Success:   true,
		Message:   "Causality check complete",
		Data:      causalityResp,
		Timestamp: time.Now().UTC(),
	})
}

// BatchRetrieve handles GET /api/v1/events
func (h *Handler) BatchRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	
	var req models.BatchQueryRequest

	// Parse note_id
	if noteIDStr := query.Get("note_id"); noteIDStr != "" {
		noteID, err := strconv.Atoi(noteIDStr)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "note_id must be a valid integer")
			return
		}
		req.NoteID = &noteID
	}

	// Parse user_id
	if userID := query.Get("user_id"); userID != "" {
		req.UserID = &userID
	}

	// Parse start_seq and end_seq
	if startSeqStr := query.Get("start_seq"); startSeqStr != "" {
		startSeq, err := strconv.ParseUint(startSeqStr, 10, 64)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "start_seq must be a valid sequence number")
			return
		}
		req.StartSeq = &startSeq
	}

	if endSeqStr := query.Get("end_seq"); endSeqStr != "" {
		endSeq, err := strconv.ParseUint(endSeqStr, 10, 64)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "end_seq must be a valid sequence number")
			return
		}
		req.EndSeq = &endSeq
	}

	// Parse start_time and end_time (ms since epoch)
	if startTimeStr := query.Get("start_time"); startTimeStr != "" {
		startTime, err := strconv.ParseInt(startTimeStr, 10, 64)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "start_time must be a valid timestamp")
			return
		}
		req.StartTime = &startTime
	}

	if endTimeStr := query.Get("end_time"); endTimeStr != "" {
		endTime, err := strconv.ParseInt(endTimeStr, 10, 64)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "end_time must be a valid timestamp")
			return
		}
		req.EndTime = &endTime
	}

	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			h.writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		req.Limit = limit
	}

	log.Printf("[Gateway] Batch retrieval: note_id=%v, user_id=%v, limit=%d", 
		req.NoteID, req.UserID, req.Limit)

	// Query database
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	events, totalCount, err := h.dbClient.BatchQuery(ctx, req)
	if err != nil {
		log.Printf("[Gateway] Failed to query events: %v", err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query events: %v", err))
		return
	}

	// Build response
	batchResp := models.BatchQueryResponse{
		Events:     events,
		TotalCount: totalCount,
	}

	log.Printf("[Gateway] Batch retrieval complete: %d events returned", totalCount)

	h.writeJSON(w, http.StatusOK, models.StandardResponse{
		Success:   true,
		Message:   fmt.Sprintf("Retrieved %d events", totalCount),
		Data:      batchResp,
		Timestamp: time.Now().UTC(),
	})
}

// HealthCheck handles GET /health
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Test database connection
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	_, _, err := h.dbClient.BatchQuery(ctx, models.BatchQueryRequest{Limit: 1})
	dbHealthy := err == nil

	h.writeJSON(w, http.StatusOK, models.StandardResponse{
		Success:   true,
		Message:   "API Gateway is healthy",
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"gateway_ready":  true,
			"database_healthy": dbHealthy,
			"boundary_url":   h.boundaryURL,
		},
	})
}

// callBoundaryAdapter calls the Boundary Adapter to ingest an event
func (h *Handler) callBoundaryAdapter(ctx context.Context, event models.BoundaryEvent) (*models.BoundaryResponse, error) {
	// Serialize event
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", h.boundaryURL+"/ingest", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call boundary adapter: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("boundary adapter returned error: %s", string(respBody))
	}

	// Parse response
	var boundaryResp models.BoundaryResponse
	if err := json.Unmarshal(respBody, &boundaryResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &boundaryResp, nil
}

// writeJSON writes a JSON response
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[Gateway] Error encoding JSON response: %v", err)
	}
}

// writeError writes an error response
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	response := models.StandardResponse{
		Success:   false,
		Error:     message,
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, status, response)
}

// RegisterRoutes sets up HTTP routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.HealthCheck)
	mux.HandleFunc("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.SubmitEvent(w, r)
		} else if r.Method == http.MethodGet {
			h.BatchRetrieve(w, r)
		} else {
			h.writeError(w, http.StatusMethodNotAllowed, "only GET and POST methods are allowed")
		}
	})
	mux.HandleFunc("/api/v1/causality", h.CheckCausality)
}
