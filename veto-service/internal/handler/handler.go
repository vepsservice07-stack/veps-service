package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/veps-service-480701/veto-service/internal/validator"
	"github.com/veps-service-480701/veto-service/pkg/models"
)

// Handler manages HTTP requests for the Veto Service
type Handler struct {
	validator *validator.Validator
}

// New creates a new HTTP handler
func New(v *validator.Validator) *Handler {
	return &Handler{
		validator: v,
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
	response := Response{
		Success:   true,
		Message:   "Veto Service is healthy",
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, http.StatusOK, response)
}

// ValidateEvent handles event validation requests from Boundary Adapter
func (h *Handler) ValidateEvent(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse veto request from request body
	var vetoRequest models.VetoRequest
	if err := json.NewDecoder(r.Body).Decode(&vetoRequest); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Validate the event structure
	if vetoRequest.Event.ID.String() == "" {
		h.writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	if vetoRequest.Event.Type == "" {
		h.writeError(w, http.StatusBadRequest, "event type is required")
		return
	}

	log.Printf("[Handler] Validating event %s (type: %s)", vetoRequest.Event.ID, vetoRequest.Event.Type)

	// Perform validation
	passed, validationErrors, err := h.validator.Validate(r.Context(), vetoRequest.Event)
	if err != nil {
		log.Printf("[Handler] Validation error for event %s: %v", vetoRequest.Event.ID, err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("validation failed: %v", err))
		return
	}

	duration := time.Since(startTime)

	// Prepare failed checks list
	failedChecks := []string{}
	reasons := []string{}
	for _, verr := range validationErrors {
		failedChecks = append(failedChecks, verr.Check)
		reasons = append(reasons, fmt.Sprintf("%s: %s", verr.Check, verr.Reason))
	}

	if !passed {
		// Validation failed - veto the event
		log.Printf("[Handler] Event %s VETOED: %v", vetoRequest.Event.ID, reasons)
		
		response := Response{
			Success:   false,
			Message:   "Event validation failed - VETOED",
			EventID:   vetoRequest.Event.ID.String(),
			Timestamp: time.Now().UTC(),
			Duration:  duration.String(),
			Data: map[string]interface{}{
				"passed":        false,
				"failed_checks": failedChecks,
				"reasons":       reasons,
			},
		}
		h.writeJSON(w, http.StatusPreconditionFailed, response)
		return
	}

	// Validation passed
	log.Printf("[Handler] Event %s PASSED validation in %s", vetoRequest.Event.ID, duration)

	response := Response{
		Success:   true,
		Message:   "Event validation passed",
		EventID:   vetoRequest.Event.ID.String(),
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data: map[string]interface{}{
			"passed": true,
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
	mux.HandleFunc("/validate", h.ValidateEvent)
}