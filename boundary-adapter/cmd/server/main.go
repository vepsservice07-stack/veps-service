package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/veps-service-480701/boundary-adapter/internal/client"
	"github.com/veps-service-480701/boundary-adapter/internal/handler"
	"github.com/veps-service-480701/boundary-adapter/internal/normalizer"
	"github.com/veps-service-480701/boundary-adapter/internal/router"
)

func main() {
	log.Println("[Main] Starting VEPS Boundary Adapter...")

	// Load configuration from environment
	config := loadConfig()

	// Initialize components
	norm := normalizer.New()
	log.Printf("[Main] Normalizer initialized with node ID: %s", os.Getenv("VEPS_NODE_ID"))

	// Initialize real service clients
	rdbClient := client.NewRDBClient(config.RDBUpdaterURL, 5*time.Second)
	vetoClient := client.NewVetoClient(config.VetoServiceURL, 5*time.Second)

	log.Printf("[Main] Service clients initialized (RDB: %s, Veto: %s)", 
		config.RDBUpdaterURL, config.VetoServiceURL)

	// Initialize router with timeout for sub-50ms requirement
	rtr := router.New(vetoClient, rdbClient, config.RouterTimeout)
	log.Printf("[Main] Router initialized with %s timeout", config.RouterTimeout)

	// Initialize HTTP handler
	h := handler.New(norm, rtr)

	// Set up HTTP server
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Add middleware for logging and CORS
	wrappedMux := loggingMiddleware(corsMiddleware(mux))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", config.Port),
		Handler:      wrappedMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("[Main] Server listening on port %s", config.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[Main] Shutdown signal received, gracefully shutting down...")

	// Give outstanding requests 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("[Main] Server forced to shutdown: %v", err)
	}

	log.Println("[Main] Server exited successfully")
}

// Config holds application configuration
type Config struct {
	Port           string
	RouterTimeout  time.Duration
	NodeID         string
	RDBUpdaterURL  string
	VetoServiceURL string
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port for Cloud Run
	}

	timeoutStr := os.Getenv("ROUTER_TIMEOUT_MS")
	timeout := 50 * time.Millisecond // Default to 50ms for sub-50ms requirement
	if timeoutStr != "" {
		if ms, err := time.ParseDuration(timeoutStr + "ms"); err == nil {
			timeout = ms
		}
	}

	nodeID := os.Getenv("VEPS_NODE_ID")
	if nodeID == "" {
		// Generate a default node ID if not set
		nodeID = fmt.Sprintf("boundary-adapter-%d", time.Now().Unix())
		os.Setenv("VEPS_NODE_ID", nodeID)
	}

	rdbUpdaterURL := os.Getenv("RDB_UPDATER_URL")
	if rdbUpdaterURL == "" {
		rdbUpdaterURL = "http://localhost:8081" // Default for local dev
	}

	vetoServiceURL := os.Getenv("VETO_SERVICE_URL")
	if vetoServiceURL == "" {
		vetoServiceURL = "http://localhost:8082" // Default for local dev
	}

	return Config{
		Port:           port,
		RouterTimeout:  timeout,
		NodeID:         nodeID,
		RDBUpdaterURL:  rdbUpdaterURL,
		VetoServiceURL: vetoServiceURL,
	}
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		next.ServeHTTP(wrapped, r)
		
		duration := time.Since(start)
		log.Printf(
			"[HTTP] %s %s - Status: %d - Duration: %s - RemoteAddr: %s",
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			duration,
			r.RemoteAddr,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}