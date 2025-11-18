package engine

import (
	"context"
	"fmt"
)

// Provider describes an abstract LLM or tool executor.
type Provider interface {
	CallLLM(ctx context.Context, step StepDef, input string) (string, error)
}

// ProviderRegistry stores Provider implementations keyed by their profile ID.
type ProviderRegistry struct {
	providers map[ProviderProfileID]Provider
}

// NewProviderRegistry returns an empty provider registry ready for registration.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: map[ProviderProfileID]Provider{}}
}

// Register stores the provided implementation in the registry.
func (r *ProviderRegistry) Register(id ProviderProfileID, provider Provider) {
	r.providers[id] = provider
}

// Get returns the provider assigned to the given ID, or nil when missing.
func (r *ProviderRegistry) Get(id ProviderProfileID) Provider {
	return r.providers[id]
}

// OllamaProvider is a stub for future Ollama integrations.
type OllamaProvider struct {
	BaseURI string
	Model   string
}

// CallLLM currently returns a dummy value to satisfy the interface.
func (p *OllamaProvider) CallLLM(ctx context.Context, step StepDef, input string) (string, error) {
	// TODO: use PromptTemplate to build the final payload.
	// TODO: invoke Ollama's HTTP API.
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return fmt.Sprintf("dummy response from ollama for step %s", step.ID), nil
}
