package main

import (
	"context"
	"testing"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/store"
)

type fakeEngine struct {
	regs []engine.PipelineDef
}

func (f *fakeEngine) RunJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
	return nil, nil
}

func (f *fakeEngine) RunJobStream(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
	return nil, nil, nil
}

func (f *fakeEngine) CancelJob(ctx context.Context, jobID string, reason string) error {
	return nil
}

func (f *fakeEngine) GetJob(ctx context.Context, jobID string) (*engine.Job, error) {
	return nil, nil
}

func (f *fakeEngine) RegisterPipeline(def engine.PipelineDef) {
	f.regs = append(f.regs, def)
}

func (f *fakeEngine) UpsertProviderProfile(profile engine.ProviderProfile) error {
	return nil
}

func (f *fakeEngine) ListPipelines() []engine.PipelineDef {
	return append([]engine.PipelineDef{}, f.regs...)
}

func TestBuildOpenAIProfileFromEnv(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		t.Setenv(engine.OpenAIAPIKeyEnvVar, "")
		if _, ok := buildOpenAIProfileFromEnv(); ok {
			t.Fatalf("expected missing key to disable profile")
		}
	})

	t.Run("with env", func(t *testing.T) {
		t.Setenv(engine.OpenAIAPIKeyEnvVar, "sk-test")
		t.Setenv("PIPELINE_ENGINE_OPENAI_BASE_URL", "http://local.openai")
		t.Setenv("PIPELINE_ENGINE_OPENAI_MODEL", "gpt-x")

		profile, ok := buildOpenAIProfileFromEnv()
		if !ok {
			t.Fatalf("expected profile to be built")
		}
		if profile.BaseURI != "http://local.openai" {
			t.Fatalf("unexpected base uri: %s", profile.BaseURI)
		}
		if profile.DefaultModel != "gpt-x" {
			t.Fatalf("unexpected model: %s", profile.DefaultModel)
		}
	})
}

func TestBuildOllamaProfileFromEnv(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		t.Setenv(engine.OllamaEnableEnvVar, "")
		t.Setenv(engine.OllamaBaseURLEnvVar, "")
		if _, ok := buildOllamaProfileFromEnv(); ok {
			t.Fatalf("expected profile to be disabled")
		}
	})

	t.Run("enabled", func(t *testing.T) {
		t.Setenv(engine.OllamaEnableEnvVar, "1")
		t.Setenv(engine.OllamaBaseURLEnvVar, "http://ollama.local")
		t.Setenv(engine.OllamaModelEnvVar, "llama-test")

		profile, ok := buildOllamaProfileFromEnv()
		if !ok {
			t.Fatalf("expected profile to be built")
		}
		if profile.BaseURI != "http://ollama.local" {
			t.Fatalf("unexpected base uri: %s", profile.BaseURI)
		}
		if profile.DefaultModel != "llama-test" {
			t.Fatalf("unexpected model: %s", profile.DefaultModel)
		}
	})
}

func TestBuildEngineRegistersProfiles(t *testing.T) {
	t.Setenv(engine.OpenAIAPIKeyEnvVar, "sk-test")
	t.Setenv(engine.OllamaEnableEnvVar, "1")

	eng, runtime := buildEngine(store.NewMemoryStore())
	if runtime.openAIProfileID == nil {
		t.Fatalf("expected openai profile runtime to be set")
	}
	if runtime.ollamaProfileID == nil {
		t.Fatalf("expected ollama profile runtime to be set")
	}
	if _, ok := eng.(*engine.BasicEngine); !ok {
		t.Fatalf("expected engine to be *engine.BasicEngine")
	}
}

func TestRegisterDemoPipelines(t *testing.T) {
	fake := &fakeEngine{}
	openID := engine.ProviderProfileID("openai-cli")
	ollamaID := engine.ProviderProfileID("ollama-cli")

	registerDemoPipelines(fake, providerRuntime{openAIProfileID: &openID, ollamaProfileID: &ollamaID})

	if len(fake.regs) != 4 {
		t.Fatalf("expected 4 demo pipelines, got %d", len(fake.regs))
	}

	expected := map[engine.PipelineType]bool{
		engine.PipelineType("openai.summarize.v1"):   true,
		engine.PipelineType("openai.chain.v1"):       true,
		engine.PipelineType("openai.funmarkdown.v1"): true,
		engine.PipelineType("ollama.summarize.v1"):   true,
	}
	for _, def := range fake.regs {
		if !expected[def.Type] {
			t.Fatalf("unexpected pipeline type registered: %s", def.Type)
		}
		delete(expected, def.Type)
	}
	if len(expected) != 0 {
		t.Fatalf("missing pipelines: %v", expected)
	}
}
