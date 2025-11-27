package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/pipeline-engine/pkg/mcp/adapter"
	gosdk "github.com/example/pipeline-engine/pkg/sdk/go"
)

func main() {
	var (
		addr = flag.String("addr", "", "Pipeline Engine base URL (default PIPELINE_ENGINE_ADDR or http://127.0.0.1:8085)")
	)
	flag.Parse()

	baseURL := *addr
	if baseURL == "" {
		baseURL = os.Getenv("PIPELINE_ENGINE_ADDR")
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8085"
	}

	logger := log.New(os.Stderr, "[mcp-adapter] ", log.LstdFlags)
	client := gosdk.NewClient(baseURL)
	sdkClient := adapter.NewSDKClient(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			cancel()
		case <-ctx.Done():
		}
	}()

	adp := adapter.NewAdapter(adapter.Options{
		Client: sdkClient,
		Logger: logger,
	})
	if err := adp.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "adapter terminated: %v\n", err)
		os.Exit(1)
	}
}
