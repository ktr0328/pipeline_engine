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

	"github.com/example/pipeline-engine/internal/server"
	"github.com/example/pipeline-engine/internal/store"
	"github.com/example/pipeline-engine/pkg/logging"
)

func main() {
	addr := ":8085"
	if env := os.Getenv("PIPELINE_ENGINE_ADDR"); env != "" {
		addr = env
	}
	level := logging.SetLevelFromString(os.Getenv("PIPELINE_ENGINE_LOG_LEVEL"))
	logging.Infof("log level configured: %s", level.String())

	jobStore := store.NewMemoryStore()
	eng, providers := buildEngine(jobStore)
	registerDemoPipelines(eng, providers)
	srv := server.NewServer(eng)
	logEnvStatus(providers)

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

	logging.Infof("pipeline engine listening on %s", addr)
	if err := srv.ListenAndServe(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}
