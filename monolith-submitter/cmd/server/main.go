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

	"github.com/veps-service-480701/monolith-submitter/internal/client"
	"github.com/veps-service-480701/monolith-submitter/internal/handler"
)

func main() {
	log.Println("[Main] Starting VEPS Monolith Submitter...")

	// Load configuration from environment
	config := loadConfig()

	// Initialize Ledger client
	ledgerClient, err := client.NewLedgerClient(
		config.LedgerAddress,
		config.SecretKey,
		config.NodeID,
	)
	if err != nil {
		log.Fatalf("[Main] Failed to initialize Ledger client: %v", err)
	}
	defer ledgerClient.Close()

	log.Printf("[Main] Ledger client initialized (address: %s)", config.LedgerAddress)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	healthy, status, lastSeq, err := ledgerClient.HealthCheck(ctx)
	cancel()

	if err != nil {
		log.Printf("[Main] Warning: Initial health check failed: %v", err)
	} else {
		log.Printf("[Main] Ledger health: %v, status: %s, last sequence: %d",
			healthy, status, lastSeq)
	}

	// Initialize HTTP handler
	h := handler.New(ledgerClient)

	// Set up HTTP server
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Add middleware
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
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("[Main] Server forced to shutdown: %v", err)
	}

	log.Println("[Main] Server exited successfully")
}

// Config holds application configuration
type Config struct {
	Port          string
	LedgerAddress string
	SecretKey     string
	NodeID        string
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port for Cloud Run
	}

	ledgerAddress := os.Getenv("LEDGER_ADDRESS")
	if ledgerAddress == "" {
		ledgerAddress = "ledger-service.immutable-ledger.svc.cluster.local:50051"
		log.Printf("[Main] Using default LEDGER_ADDRESS: %s", ledgerAddress)
	}

	secretKey := os.Getenv("VEPS_SECRET_KEY")
	if secretKey == "" {
		secretKey = "default-dev-secret-key-change-in-production"
		log.Printf("[Main] Warning: Using default secret key (set VEPS_SECRET_KEY in production)")
	}

	nodeID := os.Getenv("MONOLITH_NODE_ID")
	if nodeID == "" {
		nodeID = "monolith-submitter-us-east1-001"
		log.Printf("[Main] Using default MONOLITH_NODE_ID: %s", nodeID)
	}

	return Config{
		Port:          port,
		LedgerAddress: ledgerAddress,
		SecretKey:     secretKey,
		NodeID:        nodeID,
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
