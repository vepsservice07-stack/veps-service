package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/veps-service-480701/boundary-adapter/internal/client"
	"github.com/veps-service-480701/boundary-adapter/internal/normalizer"
	"github.com/veps-service-480701/boundary-adapter/internal/router"
	"github.com/veps-service-480701/boundary-adapter/pkg/models"
)

// Handler manages HTTP requests for the Boundary Adapter
type Handler struct {
	normalizer *normalizer.Normalizer
	router     *router.Router
}

// New creates a new HTTP handler
func New(norm *normalizer.Normalizer, rtr *router.Router) *Handler {
	return &Handler{
		normalizer: norm,
		router:     rtr,
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
		Message:   "Boundary Adapter is healthy",
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, http.StatusOK, response)
}

// Warmup pre-caches tokens and establishes connections
func (h *Handler) Warmup(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	
	// Pre-cache authentication tokens
	vetoURL := os.Getenv("VETO_SERVICE_URL")
	rdbURL := os.Getenv("RDB_UPDATER_URL")
	
	if vetoURL != "" {
		client.GetIDToken(ctx, vetoURL)
	}
	
	if rdbURL != "" {
		client.GetIDToken(ctx, rdbURL)
	}
	
	response := Response{
		Success:   true,
		Message:   "Warmup complete - tokens cached, connections established",
		Timestamp: time.Now().UTC(),
	}
	h.writeJSON(w, http.StatusOK, response)
}

// IngestEvent handles incoming event ingestion
func (h *Handler) IngestEvent(w http.ResponseWriter, r *http.Request) {
	// Initialize performance metrics
	metrics := &PerformanceMetrics{
		StartTime: time.Now(),
		Timestamps: make(map[string]time.Time),
		Breakdowns: make(map[string]time.Duration),
	}
	metrics.Timestamps["request_received"] = time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse raw event from request body
	var rawEvent models.RawEvent
	if err := json.NewDecoder(r.Body).Decode(&rawEvent); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()
	
	metrics.Timestamps["request_parsed"] = time.Now()

	// Validate schema
	if err := h.normalizer.ValidateSchema(rawEvent); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("schema validation failed: %v", err))
		return
	}

	// Normalize the event
	event, err := h.normalizer.Normalize(rawEvent)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("normalization failed: %v", err))
		return
	}
	
	metrics.Timestamps["event_normalized"] = time.Now()

	log.Printf("[Handler] Normalized event %s from source %s", event.ID, event.Source)

	metrics.Timestamps["routing_started"] = time.Now()
	
	// Route through concurrent split
	routeResult, err := h.router.Route(r.Context(), *event)
	if err != nil {
		// Routing failed - likely veto service rejected or timeout
		log.Printf("[Handler] Routing failed for event %s: %v", event.ID, err)
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("event processing failed: %v", err))
		return
	}
	
	metrics.Timestamps["routing_completed"] = time.Now()

	// Calculate detailed breakdown
	metrics.EndTime = time.Now()
	metrics.TotalDuration = metrics.EndTime.Sub(metrics.StartTime)
	
	// Calculate breakdowns
	if parsed, ok := metrics.Timestamps["request_parsed"]; ok {
		if received, ok := metrics.Timestamps["request_received"]; ok {
			metrics.Breakdowns["parsing_ms"] = parsed.Sub(received)
		}
	}
	
	if normalized, ok := metrics.Timestamps["event_normalized"]; ok {
		if parsed, ok := metrics.Timestamps["request_parsed"]; ok {
			metrics.Breakdowns["normalization_ms"] = normalized.Sub(parsed)
		}
	}
	
	if routeEnd, ok := metrics.Timestamps["routing_completed"]; ok {
		if routeStart, ok := metrics.Timestamps["routing_started"]; ok {
			metrics.Breakdowns["routing_total_ms"] = routeEnd.Sub(routeStart)
		}
	}
	
	// Success response with detailed timing
	response := Response{
		Success:   true,
		Message:   "Event ingested and routed successfully",
		EventID:   event.ID.String(),
		Timestamp: time.Now().UTC(),
		Duration:  metrics.TotalDuration.String(),
		Data: map[string]interface{}{
			"event":             event,
			"integrity_success": routeResult.IntegritySuccess,
			"context_success":   routeResult.ContextSuccess,
			"routing_duration":  routeResult.Duration.String(),
			"performance_breakdown": map[string]interface{}{
				"total_ms":         float64(metrics.TotalDuration.Microseconds()) / 1000.0,
				"parsing_ms":       float64(metrics.Breakdowns["parsing_ms"].Microseconds()) / 1000.0,
				"normalization_ms": float64(metrics.Breakdowns["normalization_ms"].Microseconds()) / 1000.0,
				"routing_ms":       float64(metrics.Breakdowns["routing_total_ms"].Microseconds()) / 1000.0,
				"veps_internal_ms": float64((metrics.Breakdowns["normalization_ms"] + metrics.Breakdowns["routing_total_ms"]).Microseconds()) / 1000.0,
			},
		},
	}

	log.Printf("[Handler] Event %s processed successfully - Total: %.2fms, VEPS Internal: %.2fms", 
		event.ID, 
		float64(metrics.TotalDuration.Microseconds())/1000.0,
		float64((metrics.Breakdowns["normalization_ms"] + metrics.Breakdowns["routing_total_ms"]).Microseconds())/1000.0)
	h.writeJSON(w, http.StatusOK, response)
}

// PerformanceMetrics captures detailed timing
type PerformanceMetrics struct {
	StartTime     time.Time
	EndTime       time.Time
	TotalDuration time.Duration
	Timestamps    map[string]time.Time
	Breakdowns    map[string]time.Duration
}
}

// IngestBatch handles batch event ingestion
func (h *Handler) IngestBatch(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Parse batch of raw events
	var rawEvents []models.RawEvent
	if err := json.NewDecoder(r.Body).Decode(&rawEvents); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	defer r.Body.Close()

	if len(rawEvents) == 0 {
		h.writeError(w, http.StatusBadRequest, "batch cannot be empty")
		return
	}

	if len(rawEvents) > 100 {
		h.writeError(w, http.StatusBadRequest, "batch size exceeds maximum of 100 events")
		return
	}

	// Normalize all events
	events := make([]models.Event, 0, len(rawEvents))
	for i, rawEvent := range rawEvents {
		if err := h.normalizer.ValidateSchema(rawEvent); err != nil {
			h.writeError(w, http.StatusBadRequest, fmt.Sprintf("event %d schema validation failed: %v", i, err))
			return
		}

		event, err := h.normalizer.Normalize(rawEvent)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, fmt.Sprintf("event %d normalization failed: %v", i, err))
			return
		}
		events = append(events, *event)
	}

	// Route batch with concurrency control
	results := h.router.RouteBatch(r.Context(), events, 10)

	// Collect results
	successCount := 0
	failCount := 0
	for _, result := range results {
		if result != nil && result.IntegritySuccess {
			successCount++
		} else {
			failCount++
		}
	}

	duration := time.Since(startTime)

	response := Response{
		Success:   failCount == 0,
		Message:   fmt.Sprintf("Batch processing complete: %d succeeded, %d failed", successCount, failCount),
		Timestamp: time.Now().UTC(),
		Duration:  duration.String(),
		Data: map[string]interface{}{
			"total":        len(rawEvents),
			"succeeded":    successCount,
			"failed":       failCount,
			"avg_duration": duration / time.Duration(len(rawEvents)),
		},
	}

	statusCode := http.StatusOK
	if failCount > 0 {
		statusCode = http.StatusMultiStatus
	}

	log.Printf("[Handler] Batch processed: %d/%d succeeded in %s", successCount, len(rawEvents), duration)
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
	mux.HandleFunc("/warmup", h.Warmup)
	mux.HandleFunc("/ingest", h.IngestEvent)
	mux.HandleFunc("/ingest/batch", h.IngestBatch)
}