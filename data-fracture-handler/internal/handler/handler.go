package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/veps-service-480701/data-fracture-handler/internal/storage"
	"github.com/veps-service-480701/data-fracture-handler/pkg/models"
)

// Handler manages HTTP requests for the Data Fracture Handler
type Handler struct {
	storage *storage.CloudStorageWriter
}

// New creates a new HTTP handler
func New(storage *storage.CloudStorageWriter) *Handler {
	return &Handler{
		storage: storage,
	}
}

// Response represents the standard API response format
type Response struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message,omitempty"`
	FractureID string      `json:"fracture_id,omitempty"`
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// HealthCheck handles health check requests
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := Response{
		Success:   true,
		Message:   "Data Fracture Handler is healthy",
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, http.StatusOK, response)
}

// LogFracture handles incoming vetoed events
func (h *Handler) LogFracture(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse fracture request
	var fractureReq models.FractureRequest
	if err := json.NewDecoder(r.Body).Decode(&fractureReq); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	// Validate request
	if fractureReq.Event.ID.String() == "" {
		h.writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	if len(fractureReq.FailedChecks) == 0 {
		h.writeError(w, http.StatusBadRequest, "failed_checks is required")
		return
	}

	// Convert to fractured event
	fracturedEvent := fractureReq.ToFracturedEvent()

	log.Printf("[Handler] Logging fracture for event %s (checks failed: %v)", 
		fracturedEvent.Event.ID, fracturedEvent.Rejection.FailedChecks)

	// Write to Cloud Storage (async to not block response)
	go func() {
		ctx := r.Context()
		if err := h.storage.WriteFracture(ctx, fracturedEvent); err != nil {
			log.Printf("[Handler] ERROR: Failed to write fracture %s: %v", 
				fracturedEvent.FractureID, err)
		}
	}()

	duration := time.Since(startTime)

	// Return success immediately (fire-and-forget pattern)
	response := Response{
		Success:    true,
		Message:    "Fracture logged successfully",
		FractureID: fracturedEvent.FractureID.String(),
		Timestamp:  time.Now().UTC(),
		Data: map[string]interface{}{
			"event_id":      fracturedEvent.Event.ID.String(),
			"failed_checks": fracturedEvent.Rejection.FailedChecks,
			"duration":      duration.String(),
		},
	}

	log.Printf("[Handler] Fracture %s logged in %s", fracturedEvent.FractureID, duration)

	h.writeJSON(w, http.StatusOK, response)
}

// LogFractureBatch handles batch logging of vetoed events
func (h *Handler) LogFractureBatch(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse batch request
	var batchReq []models.FractureRequest
	if err := json.NewDecoder(r.Body).Decode(&batchReq); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	if len(batchReq) == 0 {
		h.writeError(w, http.StatusBadRequest, "batch cannot be empty")
		return
	}

	if len(batchReq) > 100 {
		h.writeError(w, http.StatusBadRequest, "batch size exceeds maximum of 100")
		return
	}

	// Convert all to fractured events
	fracturedEvents := make([]*models.FracturedEvent, 0, len(batchReq))
	for _, req := range batchReq {
		fracturedEvents = append(fracturedEvents, req.ToFracturedEvent())
	}

	log.Printf("[Handler] Logging batch of %d fractures", len(fracturedEvents))

	// Write batch asynchronously
	go func() {
		ctx := r.Context()
		if err := h.storage.WriteFractureBatch(ctx, fracturedEvents); err != nil {
			log.Printf("[Handler] ERROR: Failed to write fracture batch: %v", err)
		}
	}()

	duration := time.Since(startTime)

	response := Response{
		Success:   true,
		Message:   fmt.Sprintf("Batch of %d fractures logged successfully", len(fracturedEvents)),
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"count":    len(fracturedEvents),
			"duration": duration.String(),
		},
	}

	h.writeJSON(w, http.StatusOK, response)
}

// QueryFractures handles queries for fractured events by date
func (h *Handler) QueryFractures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Parse date from query parameter
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		h.writeError(w, http.StatusBadRequest, "date parameter is required (format: YYYY-MM-DD)")
		return
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid date format (expected: YYYY-MM-DD)")
		return
	}

	log.Printf("[Handler] Querying fractures for date: %s", date.Format("2006-01-02"))

	// Read fractures from Cloud Storage
	fractures, err := h.storage.ReadFractures(r.Context(), date)
	if err != nil {
		log.Printf("[Handler] ERROR: Failed to read fractures: %v", err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read fractures: %v", err))
		return
	}

	response := Response{
		Success:   true,
		Message:   fmt.Sprintf("Found %d fractures for %s", len(fractures), date.Format("2006-01-02")),
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"date":      date.Format("2006-01-02"),
			"count":     len(fractures),
			"fractures": fractures,
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
	mux.HandleFunc("/fracture", h.LogFracture)
	mux.HandleFunc("/fracture/batch", h.LogFractureBatch)
	mux.HandleFunc("/fractures", h.QueryFractures)
}
