package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProviderCall(t *testing.T) {
	sr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer sr.Close()

	profile := ProviderProfile{ID: "openai", Kind: ProviderOpenAI, BaseURI: sr.URL, APIKey: "test-key", DefaultModel: "gpt-test"}
	provider := &OpenAIProvider{profile: profile, client: sr.Client()}

	resp, err := provider.Call(context.Background(), ProviderRequest{Prompt: "hi", Profile: profile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Output != "hello" {
		t.Fatalf("unexpected output: %s", resp.Output)
	}
}

func TestOllamaProviderCall(t *testing.T) {
	sr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if payload.Model != "llama3" {
			t.Fatalf("unexpected model: %s", payload.Model)
		}
		if payload.Stream {
			t.Fatal("stream flag should be false")
		}
		if payload.System != "system" {
			t.Fatalf("unexpected system prompt: %s", payload.System)
		}
		if payload.Options == nil {
			t.Fatalf("options not forwarded: %#v", payload.Options)
		}
		if temp, ok := payload.Options["temperature"].(float64); !ok || temp != 0.1 {
			t.Fatalf("options temperature mismatch: %#v", payload.Options)
		}
		_, _ = w.Write([]byte(`{"response":"ok","model":"llama3","done":true}`))
	}))
	defer sr.Close()

	profile := ProviderProfile{ID: "ollama", Kind: ProviderOllama, BaseURI: sr.URL, DefaultModel: "llama3"}
	provider := &OllamaProvider{profile: profile, client: sr.Client()}
	reqProfile := profile
	reqProfile.Extra = map[string]any{
		"system_prompt": "system",
		"options":       map[string]any{"temperature": 0.1},
	}

	resp, err := provider.Call(context.Background(), ProviderRequest{Prompt: "hello", Profile: reqProfile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Output != "ok" {
		t.Fatalf("unexpected output: %s", resp.Output)
	}
}

func TestOllamaProviderCallHTTPError(t *testing.T) {
	sr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer sr.Close()

	profile := ProviderProfile{ID: "ollama", Kind: ProviderOllama, BaseURI: sr.URL, DefaultModel: "llama3"}
	provider := &OllamaProvider{profile: profile, client: sr.Client()}

	if _, err := provider.Call(context.Background(), ProviderRequest{Prompt: "hello", Profile: profile}); err == nil {
		t.Fatal("expected error")
	}
}
