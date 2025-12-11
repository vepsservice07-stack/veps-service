package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/veps-service-480701/api-gateway/internal/auth"
	"github.com/veps-service-480701/api-gateway/internal/database"
	"github.com/veps-service-480701/api-gateway/internal/handler"
	"github.com/veps-service-480701/api-gateway/internal/secrets"
)

func main() {
	log.Println("[Main] Starting VEPS API Gateway...")

	// Load configuration
	config := loadConfig()

	// Initialize authentication
	log.Println("[Main] Initializing authentication...")
	keyStore := auth.NewKeyStore(config.ProjectID)
	rateLimiter := auth.NewRateLimiter()

	// Initialize database client
	dbClient, err := database.NewClient(config.DatabaseURL)
	if err != nil {
		log.Fatalf("[Main] Failed to initialize database client: %v", err)
	}
	defer dbClient.Close()

	log.Printf("[Main] Database client initialized")

	// Initialize HTTP handler
	h := handler.New(config.BoundaryURL, dbClient)

	// Set up HTTP server
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Add middleware (auth -> logging -> cors)
	wrappedMux := loggingMiddleware(auth.Middleware(keyStore, rateLimiter)(corsMiddleware(mux)))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", config.Port),
		Handler:      wrappedMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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
	Port         string
	BoundaryURL  string
	DatabaseURL  string
	ProjectID    string
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	boundaryURL := os.Getenv("BOUNDARY_ADAPTER_URL")
	if boundaryURL == "" {
		log.Fatal("[Main] BOUNDARY_ADAPTER_URL environment variable is required")
	}

	// Get project ID for Secret Manager
	projectID := os.Getenv("GCP_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		log.Fatal("[Main] GCP_PROJECT or GOOGLE_CLOUD_PROJECT environment variable is required")
	}

	// Get database password from Secret Manager
	log.Println("[Main] Retrieving database password from Secret Manager...")
	dbPassword, err := secrets.GetSecret(projectID, "veps-db-password")
	if err != nil {
		log.Fatalf("[Main] Failed to get database password from Secret Manager: %v", err)
	}

	// Build database connection string
	dbInstance := os.Getenv("DB_INSTANCE")
	if dbInstance == "" {
		dbInstance = fmt.Sprintf("%s:us-east1:veps-db", projectID)
	}

	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "veps_user"
	}

	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "veps_db"
	}

	databaseURL := fmt.Sprintf("host=/cloudsql/%s user=%s password=%s dbname=%s sslmode=disable",
		dbInstance, dbUser, dbPassword, dbName)

	log.Printf("[Main] Configuration loaded:")
	log.Printf("  Port: %s", port)
	log.Printf("  Boundary Adapter: %s", boundaryURL)
	log.Printf("  Database: %s", maskConnectionString(databaseURL))
	log.Printf("  Project: %s", projectID)

	return Config{
		Port:        port,
		BoundaryURL: boundaryURL,
		DatabaseURL: databaseURL,
		ProjectID:   projectID,
	}
}

// maskConnectionString masks sensitive parts of the connection string
func maskConnectionString(connStr string) string {
	// Mask password in connection string
	masked := connStr
	if idx := strings.Index(connStr, "password="); idx != -1 {
		start := idx + 9 // length of "password="
		end := strings.Index(connStr[start:], " ")
		if end == -1 {
			masked = connStr[:start] + "***"
		} else {
			masked = connStr[:start] + "***" + connStr[start+end:]
		}
	}
	return masked
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

// corsMiddleware adds CORS headers
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
