package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
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
