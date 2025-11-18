package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down pipeline engine")
	}()

	log.Printf("pipeline engine listening on %s\n", addr)
	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
