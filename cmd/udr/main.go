package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"udr-test/internal/api"
	"udr-test/internal/datastore"
)

func main() {
	log.Println("Starting UDR test server...")

	// Get configuration from environment variables
	mongoURI := getEnv("MONGODB_URI", "mongodb://localhost:27017")
	dbName := getEnv("MONGODB_DB", "udr")
	port := getEnv("PORT", "8080")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize MongoDB datastore
	db, err := datastore.NewDatastore(ctx, mongoURI, dbName)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := db.Close(cleanupCtx); err != nil {
			log.Printf("Error closing MongoDB connection: %v", err)
		}
	}()

	// Initialize UDR Server and Router
	server := api.NewServer(db)
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Register OpenAPI Handlers
	// Since 3GPP APIs use /nudr-dr/v1 as base path, Chi router handles routing.
	// But let's check if the generated paths include the base path /nudr-dr/v1.
	// Yes! In 3GPP OpenAPI files, paths do not include the base path,
	// but the oapi-codegen router matches whatever path is defined in the Swagger specification.
	// In TS29504_Nudr_DR.yaml, the path templates start with /subscription-data/{ueId}...
	// The basePath is /nudr-dr/v1.
	// So we can mount the generated handler under /nudr-dr/v1 !
	h := api.HandlerFromMux(server, chi.NewRouter())

	r.Mount("/nudr-dr/v1", h)

	// Add a root healthcheck
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"UP"}`))
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server gracefully...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server Shutdown error: %v", err)
		}
	}()

	log.Printf("UDR Server is running on port %s", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server ListenAndServe error: %v", err)
	}
	log.Println("Server stopped.")
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}
