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
	eng, providers := buildEngine(jobStore)
	registerDemoPipelines(eng, providers)
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

type providerRuntime struct {
	openAIProfileID *engine.ProviderProfileID
}

func buildEngine(jobStore engine.JobStore) (engine.Engine, providerRuntime) {
	profiles := []engine.ProviderProfile{}
	var runtime providerRuntime
	if profile, ok := buildOpenAIProfileFromEnv(); ok {
		profiles = append(profiles, profile)
		id := profile.ID
		runtime.openAIProfileID = &id
	}

	if len(profiles) > 0 {
		return engine.NewBasicEngineWithConfig(jobStore, &engine.EngineConfig{Providers: profiles}), runtime
	}
	return engine.NewBasicEngine(jobStore), runtime
}

func buildOpenAIProfileFromEnv() (engine.ProviderProfile, bool) {
	apiKey := os.Getenv(engine.OpenAIAPIKeyEnvVar)
	if apiKey == "" {
		return engine.ProviderProfile{}, false
	}
	base := os.Getenv("PIPELINE_ENGINE_OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := os.Getenv("PIPELINE_ENGINE_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	profile := engine.ProviderProfile{
		ID:           engine.ProviderProfileID("openai-cli"),
		Kind:         engine.ProviderOpenAI,
		BaseURI:      base,
		APIKey:       apiKey,
		DefaultModel: model,
	}
	return profile, true
}

func registerDemoPipelines(eng engine.Engine, providers providerRuntime) {
	registrar, ok := eng.(interface{ RegisterPipeline(engine.PipelineDef) })
	if !ok {
		return
	}
	if providers.openAIProfileID != nil {
		registrar.RegisterPipeline(engine.PipelineDef{
			Type:    engine.PipelineType("openai.summarize.v1"),
			Version: "v1",
			Steps: []engine.StepDef{
				{
					ID:                engine.StepID("summarize"),
					Name:              "OpenAI Summarize",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					ProviderProfileID: *providers.openAIProfileID,
					OutputType:        engine.ContentText,
					Export:            true,
				},
			},
		})
		log.Println("registered demo pipeline openai.summarize.v1 for profile", *providers.openAIProfileID)
	}
}
