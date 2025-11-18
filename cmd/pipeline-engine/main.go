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

	log.Printf("pipeline engine listening on %s\n", addr)
	if err := srv.ListenAndServe(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}

func logEnvStatus(runtime providerRuntime) {
	if runtime.openAIProfileID != nil {
		log.Printf("OpenAI provider enabled via %s (profile %s)", engine.OpenAIAPIKeyEnvVar, *runtime.openAIProfileID)
	} else {
		log.Printf("OpenAI provider disabled (%s not set)", engine.OpenAIAPIKeyEnvVar)
	}
	if runtime.ollamaProfileID != nil {
		log.Printf("Ollama provider enabled (profile %s)", *runtime.ollamaProfileID)
	} else {
		log.Printf("Ollama provider disabled (set %s or %s)", engine.OllamaEnableEnvVar, engine.OllamaBaseURLEnvVar)
	}
}

type providerRuntime struct {
	openAIProfileID *engine.ProviderProfileID
	ollamaProfileID *engine.ProviderProfileID
}

func buildEngine(jobStore engine.JobStore) (engine.Engine, providerRuntime) {
	profiles := []engine.ProviderProfile{}
	var runtime providerRuntime
	if profile, ok := buildOpenAIProfileFromEnv(); ok {
		profiles = append(profiles, profile)
		id := profile.ID
		runtime.openAIProfileID = &id
	}
	if profile, ok := buildOllamaProfileFromEnv(); ok {
		profiles = append(profiles, profile)
		id := profile.ID
		runtime.ollamaProfileID = &id
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

func buildOllamaProfileFromEnv() (engine.ProviderProfile, bool) {
	enabled := os.Getenv(engine.OllamaEnableEnvVar)
	base := os.Getenv(engine.OllamaBaseURLEnvVar)
	if enabled == "" && base == "" {
		return engine.ProviderProfile{}, false
	}
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	model := os.Getenv(engine.OllamaModelEnvVar)
	if model == "" {
		model = "llama3"
	}
	profile := engine.ProviderProfile{
		ID:           engine.ProviderProfileID("ollama-cli"),
		Kind:         engine.ProviderOllama,
		BaseURI:      base,
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
		registrar.RegisterPipeline(engine.PipelineDef{
			Type:    engine.PipelineType("openai.chain.v1"),
			Version: "v1",
			Steps: []engine.StepDef{
				{
					ID:                engine.StepID("summarize"),
					Name:              "Summarize Input",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					ProviderProfileID: *providers.openAIProfileID,
					Prompt: &engine.PromptTemplate{
						System: "You are a concise assistant that writes Japanese summaries when the input is Japanese.",
						User:   "Summarize the following context:\n{{range .Sources}}{{.Content}}\n{{end}}",
					},
					OutputType: engine.ContentText,
					Export:     false,
				},
				{
					ID:                engine.StepID("polish"),
					Name:              "Polish Summary",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					DependsOn:         []engine.StepID{engine.StepID("summarize")},
					ProviderProfileID: *providers.openAIProfileID,
					Prompt: &engine.PromptTemplate{
						System: "You are a meticulous proofreader. Keep the tone friendly and preserve Japanese if the input is Japanese.",
						User:   "Polish the summary below for clarity and fix typos. Output markdown.\n{{with index .Previous \"summarize\"}}{{with index . 0}}{{index .Data \"text\"}}{{end}}{{end}}",
					},
					OutputType: engine.ContentMarkdown,
					Export:     true,
				},
			},
		})
		log.Println("registered demo pipeline openai.chain.v1 for profile", *providers.openAIProfileID)
		registrar.RegisterPipeline(engine.PipelineDef{
			Type:    engine.PipelineType("openai.funmarkdown.v1"),
			Version: "v1",
			Steps: []engine.StepDef{
				{
					ID:                engine.StepID("trivia"),
					Name:              "Random Trivia",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					ProviderProfileID: *providers.openAIProfileID,
					Prompt: &engine.PromptTemplate{
						System: "You are a cheerful Japanese trivia guide. Speak naturally but keep lightweight labels.",
						User:   `ユーザーリクエスト:{{range .Sources}}\n- {{if .Label}}{{.Label}}: {{end}}{{.Content}}{{end}}\n\n以下の順番で回答してください。最初に口語の導入文を 1-2 文、その後に各ラベルを 1 行ずつ記述します。\n1. 口語導入 (例: そういえば… で始める)\n2. タイトル: <8文字程度>\n3. まとめ: <2文で事実と背景>\n4. 理由: <なぜ面白いか 1 文>\n5. ディテール: <音/匂い/触感など 1 文>`,
					},
					OutputType: engine.ContentText,
					Export:     true,
				},
				{
					ID:                engine.StepID("enrich"),
					Name:              "Enrich Trivia",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					DependsOn:         []engine.StepID{engine.StepID("trivia")},
					ProviderProfileID: *providers.openAIProfileID,
					Prompt: &engine.PromptTemplate{
						System: "You are an insightful narrator who deepens trivia stories while preserving their sections.",
						User: `以下のテキストを読み込み、各ラベルの内容を 10-20 文増やしつつ背景やトリビアを補足してください。出力は同じ順序とラベル (口語導入→タイトル:→まとめ:→理由:→ディテール:) のみです。

元テキスト:
{{with index .Previous "trivia"}}{{with index . 0}}{{index .Data "text"}}{{end}}{{end}}`,
					},
					OutputType: engine.ContentText,
					Export:     true,
				},
				{
					ID:                engine.StepID("markdown"),
					Name:              "Format Trivia",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					DependsOn:         []engine.StepID{engine.StepID("enrich")},
					ProviderProfileID: *providers.openAIProfileID,
					Prompt: &engine.PromptTemplate{
						System: "You are a tidy Japanese technical writer. Convert lightly structured text into a neat Markdown card.",
						User: `次のテキストを読み取り、ラベル行 (タイトル:/まとめ:/理由:/ディテール:) を抽出して Markdown に整形してください。\n- ## <タイトル>
- 冒頭の口語文を *イタリック* で引用前に挿入\n- まとめは引用 (> ) にして丁寧語へ整える\n- 理由は "### ポイント" の下で番号付き 1 行 (1.)\n- ディテールは "### ディテール" の下で箇条書き 1 行 (- )\n\n入力テキスト:\n{{with index .Previous "enrich"}}{{with index . 0}}{{index .Data "text"}}{{end}}{{end}}`,
					},
					OutputType: engine.ContentMarkdown,
					Export:     true,
				},
			},
		})
		log.Println("registered demo pipeline openai.funmarkdown.v1 for profile", *providers.openAIProfileID)
	}
	if providers.ollamaProfileID != nil {
		registrar.RegisterPipeline(engine.PipelineDef{
			Type:    engine.PipelineType("ollama.summarize.v1"),
			Version: "v1",
			Steps: []engine.StepDef{
				{
					ID:                engine.StepID("summarize"),
					Name:              "Ollama Summarize",
					Kind:              engine.StepKindLLM,
					Mode:              engine.StepModeSingle,
					ProviderProfileID: *providers.ollamaProfileID,
					OutputType:        engine.ContentText,
					Export:            true,
				},
			},
		})
		log.Println("registered demo pipeline ollama.summarize.v1 for profile", *providers.ollamaProfileID)
	}
}
