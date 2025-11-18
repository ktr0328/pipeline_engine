package engine

import (
	"context"
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
