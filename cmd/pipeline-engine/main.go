package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/server"
	"github.com/example/pipeline-engine/internal/store"
)

func main() {
	addr := ":8085"
	if env := os.Getenv("PIPELINE_ENGINE_ADDR"); env != "" {
		addr = env
	}

	jobStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(jobStore)
	srv := server.NewServer(eng)
	logEnvStatus()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down pipeline engine")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("pipeline engine listening on %s\n", addr)
	if err := srv.ListenAndServe(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}

func logEnvStatus() {
	status := []struct {
		label string
		key   string
	}{
		{label: "OpenAI", key: engine.OpenAIAPIKeyEnvVar},
	}
	for _, item := range status {
		if os.Getenv(item.key) != "" {
			log.Printf("%s API key detected via %s", item.label, item.key)
		} else {
			log.Printf("%s API key not set (%s empty)", item.label, item.key)
		}
	}
}
