package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GoCodeAlone/workflow/module"
)

// Simple standalone test server for API tests
func main() {
	// Create a UI server
	uiServer := module.NewUIServer("test-ui", ":8080", nil)

	// Start HTTP server
	server := &http.Server{
		Addr:    ":8080",
		Handler: uiServer,
	}

	// Handle server shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting test API server on :8080")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Failed to start API server: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	log.Println("Shutting down server...")

	// Allow up to 5 seconds for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Server gracefully stopped")
}