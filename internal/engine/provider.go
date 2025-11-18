package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ProviderRequest represents the context passed to concrete providers.
type ProviderRequest struct {
	Step    StepDef
	Prompt  string
	Profile ProviderProfile
	Input   ProviderInput
}

// ProviderInput shares job-level context with providers.
type ProviderInput struct {
	Sources  []Source
	Options  *JobOptions
	Previous map[StepID][]ResultItem
}

// ProviderResponse wraps a provider output payload.
type ProviderResponse struct {
	Output   string
	Metadata map[string]any
}

// Provider describes an abstract LLM / tool executor.
type Provider interface {
	Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}

// ProviderFactory instantiates a Provider using a specific profile.
type ProviderFactory func(profile ProviderProfile) Provider

// ProviderRegistry stores provider profiles and factories.
type ProviderRegistry struct {
	mu        sync.RWMutex
	profiles  map[ProviderProfileID]ProviderProfile
	factories map[ProviderKind]ProviderFactory
}

// NewProviderRegistry returns an empty provider registry ready for registration.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		profiles:  map[ProviderProfileID]ProviderProfile{},
		factories: map[ProviderKind]ProviderFactory{},
	}
}

// RegisterProfile stores the provided profile. Existing IDs are overwritten.
func (r *ProviderRegistry) RegisterProfile(profile ProviderProfile) {
	if profile.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if profile.Kind == "" {
		profile.Kind = ProviderLocal
	}
	if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	r.profiles[profile.ID] = profile
}

// RegisterFactory registers a ProviderFactory for the given kind.
func (r *ProviderRegistry) RegisterFactory(kind ProviderKind, factory ProviderFactory) {
	if kind == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[kind] = factory
}

// Resolve returns a Provider based on the Step definition.
func (r *ProviderRegistry) Resolve(step StepDef) (Provider, ProviderProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if step.ProviderProfileID == "" {
		return nil, ProviderProfile{}, errors.New("provider_profile_id is required for this step")
	}

	profile, ok := r.profiles[step.ProviderProfileID]
	if !ok {
		return nil, ProviderProfile{}, fmt.Errorf("provider profile %s not found", step.ProviderProfileID)
	}
	merged := mergeProfile(profile, step.ProviderOverride)

	factory := r.factories[merged.Kind]
	if factory == nil {
		return nil, ProviderProfile{}, fmt.Errorf("provider kind %s not registered", merged.Kind)
	}
	return factory(merged), merged, nil
}

func mergeProfile(base ProviderProfile, overrides map[string]any) ProviderProfile {
	if len(overrides) == 0 {
		return base
	}
	result := base
	if result.Extra == nil {
		result.Extra = map[string]any{}
	}
	for key, val := range overrides {
		lower := strings.ToLower(key)
		switch lower {
		case "base_uri":
			result.BaseURI = fmt.Sprint(val)
		case "api_key":
			result.APIKey = fmt.Sprint(val)
		case "default_model":
			result.DefaultModel = fmt.Sprint(val)
		default:
			result.Extra[key] = val
		}
	}
	return result
}

// RegisterDefaultProviderFactories registers stub providers for supported kinds.
func RegisterDefaultProviderFactories(reg *ProviderRegistry) {
	reg.RegisterFactory(ProviderOpenAI, func(profile ProviderProfile) Provider {
		return &OpenAIProvider{profile: profile}
	})
	reg.RegisterFactory(ProviderOllama, func(profile ProviderProfile) Provider {
		return &OllamaProvider{profile: profile}
	})
	reg.RegisterFactory(ProviderImage, func(profile ProviderProfile) Provider {
		return &ImageProvider{profile: profile}
	})
	reg.RegisterFactory(ProviderLocal, func(profile ProviderProfile) Provider {
		return &LocalToolProvider{profile: profile}
	})
}

// OpenAIProvider is a stub provider that simulates OpenAI responses.
type OpenAIProvider struct {
	profile ProviderProfile
	client  httpDoer
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func (p *OpenAIProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return callOpenAI(ctx, req, p.profile, p.httpClient())
}

func (p *OpenAIProvider) httpClient() httpDoer {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// OllamaProvider simulates Ollama responses.
type OllamaProvider struct {
	profile ProviderProfile
}

func (p *OllamaProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return simulateLLMCall(ctx, "ollama", p.profile, req)
}

// ImageProvider simulates image generation providers.
type ImageProvider struct {
	profile ProviderProfile
}

func (p *ImageProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	select {
	case <-ctx.Done():
		return ProviderResponse{}, ctx.Err()
	default:
	}
	text := fmt.Sprintf("image provider %s generated assets for step %s", p.profile.ID, req.Step.ID)
	return ProviderResponse{Output: text, Metadata: map[string]any{"provider": p.profile.Kind}}, nil
}

// LocalToolProvider simulates local shell/tool execution.
type LocalToolProvider struct {
	profile ProviderProfile
}

func (p *LocalToolProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	select {
	case <-ctx.Done():
		return ProviderResponse{}, ctx.Err()
	default:
	}
	output := fmt.Sprintf("local tool %s executed for step %s", p.profile.ID, req.Step.ID)
	return ProviderResponse{Output: output, Metadata: map[string]any{"tool": p.profile.ID}}, nil
}

func simulateLLMCall(ctx context.Context, vendor string, profile ProviderProfile, req ProviderRequest) (ProviderResponse, error) {
	select {
	case <-ctx.Done():
		return ProviderResponse{}, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	model := profile.DefaultModel
	if model == "" {
		model = vendor + "-stub"
	}
	text := fmt.Sprintf("%s (%s) responded to %s", vendor, model, req.Step.ID)
	meta := map[string]any{
		"provider": vendor,
		"model":    model,
	}
	return ProviderResponse{Output: text, Metadata: meta}, nil
}

const OpenAIAPIKeyEnvVar = "PIPELINE_ENGINE_OPENAI_API_KEY"

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func callOpenAI(ctx context.Context, req ProviderRequest, profile ProviderProfile, client httpDoer) (ProviderResponse, error) {
	model := profile.DefaultModel
	if model == "" {
		model = "gpt-4o-mini"
	}
	apiKey := profile.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(OpenAIAPIKeyEnvVar)
	}
	if apiKey == "" {
		return ProviderResponse{}, errors.New("openai api key is not configured")
	}
	base := profile.BaseURI
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	url := strings.TrimRight(base, "/") + "/chat/completions"

	messages := []openAIMessage{{Role: "user", Content: req.Prompt}}
	if sys, ok := req.Profile.Extra["system_prompt"].(string); ok && sys != "" {
		messages = append([]openAIMessage{{Role: "system", Content: sys}}, messages...)
	}
	payload := openAIRequest{Model: model, Messages: messages, Temperature: 0}
	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ProviderResponse{}, fmt.Errorf("openai api error: %s", resp.Status)
	}

	var decoded openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ProviderResponse{}, err
	}
	if len(decoded.Choices) == 0 {
		return ProviderResponse{}, errors.New("openai response missing choices")
	}

	text := decoded.Choices[0].Message.Content
	meta := map[string]any{
		"provider": "openai",
		"model":    model,
	}
	return ProviderResponse{Output: text, Metadata: meta}, nil
}

func defaultProviderProfiles() []ProviderProfile {
	return []ProviderProfile{
		{
			ID:           ProviderProfileID("default-openai"),
			Kind:         ProviderOpenAI,
			BaseURI:      "https://api.openai.com/v1",
			DefaultModel: "gpt-4o-mini",
		},
		{
			ID:           ProviderProfileID("default-ollama"),
			Kind:         ProviderOllama,
			BaseURI:      "http://127.0.0.1:11434",
			DefaultModel: "llama3",
		},
		{
			ID:      ProviderProfileID("default-image"),
			Kind:    ProviderImage,
			BaseURI: "http://localhost:9000",
		},
		{
			ID:      ProviderProfileID("default-local"),
			Kind:    ProviderLocal,
			BaseURI: "local://tool",
		},
	}
}
