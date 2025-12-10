package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/veps-service-480701/rdb-updater/internal/store"
	"github.com/veps-service-480701/rdb-updater/pkg/models"
)

// Handler manages HTTP requests for the RDB Updater
type Handler struct {
	store *store.Store
}

// New creates a new HTTP handler
func New(s *store.Store) *Handler {
	return &Handler{
		store: s,
	}
}

// Response represents the standard API response format
type Response struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message,omitempty"`
	EventID   string      `json:"event_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Duration  string      `json:"duration,omitempty"`
}

// HealthCheck handles health check requests
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Check database connectivity
	ctx := r.Context()
	if err := h.store.HealthCheck(ctx); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("database unhealthy: %v", err))
		return
	}

	response := Response{
		Success:   true,
		Message:   "RDB Updater is healthy",
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, http.StatusOK, response)
}

// UpdateContext handles context update requests from Boundary Adapter
func (h *Handler) UpdateContext(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse context update from request body
	var contextUpdate models.ContextUpdate
	if err := json.NewDecoder(r.Body).Decode(&contextUpdate); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Validate the event
	if contextUpdate.Event.ID.String() == "" {
		h.writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	if contextUpdate.Event.Type == "" {
		h.writeError(w, http.StatusBadRequest, "event type is required")
		return
	}

	// Set processed timestamp
	contextUpdate.Event.Metadata.ProcessedAt = time.Now().UTC()

	log.Printf("[Handler] Processing context update for event %s (type: %s)", 
		contextUpdate.Event.ID, contextUpdate.Event.Type)

	// Store the event based on operation type
	var err error
	switch contextUpdate.Operation {
	case "upsert", "": // Default to upsert
		err = h.store.UpsertEvent(r.Context(), contextUpdate.Event)
	default:
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported operation: %s", contextUpdate.Operation))
		return
	}

	if err != nil {
		log.Printf("[Handler] Failed to update context for event %s: %v", contextUpdate.Event.ID, err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update context: %v", err))
		return
	}

	duration := time.Since(startTime)

	// Success response
	response := Response{
		Success:   true,
		Message:   "Context updated successfully",
		EventID:   contextUpdate.Event.ID.String(),
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data: map[string]interface{}{
			"operation": contextUpdate.Operation,
			"event_type": contextUpdate.Event.Type,
		},
	}

	log.Printf("[Handler] Context updated for event %s in %s", contextUpdate.Event.ID, duration)
	h.writeJSON(w, http.StatusOK, response)
}

// GetEvent retrieves an event by ID (used by Veto Service for validation)
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept GET requests
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Get event ID from query parameter
	eventID := r.URL.Query().Get("id")
	if eventID == "" {
		h.writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	// Retrieve event from database
	event, err := h.store.GetEventByID(r.Context(), eventID)
	if err != nil {
		log.Printf("[Handler] Failed to retrieve event %s: %v", eventID, err)
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("event not found: %v", err))
		return
	}

	duration := time.Since(startTime)

	// Success response
	response := Response{
		Success:   true,
		Message:   "Event retrieved successfully",
		EventID:   event.ID.String(),
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data:      event,
	}

	log.Printf("[Handler] Event %s retrieved in %s", eventID, duration)
	h.writeJSON(w, http.StatusOK, response)
}

// CheckCausality validates vector clock causality (used by Veto Service)
func (h *Handler) CheckCausality(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse vector clock from request
	var request struct {
		VectorClock models.VectorClock `json:"vector_clock"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Check causality
	satisfied, missing, err := h.store.CheckVectorClockCausality(r.Context(), request.VectorClock)
	if err != nil {
		log.Printf("[Handler] Failed to check causality: %v", err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("causality check failed: %v", err))
		return
	}

	duration := time.Since(startTime)

	response := Response{
		Success:   satisfied,
		Message:   fmt.Sprintf("Causality check complete: satisfied=%v", satisfied),
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data: map[string]interface{}{
			"satisfied":    satisfied,
			"missing_nodes": missing,
		},
	}

	statusCode := http.StatusOK
	if !satisfied {
		statusCode = http.StatusPreconditionFailed
	}

	log.Printf("[Handler] Causality check completed in %s: satisfied=%v", duration, satisfied)
	h.writeJSON(w, statusCode, response)
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
	mux.HandleFunc("/update", h.UpdateContext)
	mux.HandleFunc("/event", h.GetEvent)
	mux.HandleFunc("/causality", h.CheckCausality)
}