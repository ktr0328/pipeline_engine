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
	"time"

	"github.com/example/pipeline-engine/pkg/logging"
)

// OpenAIProvider calls the OpenAI chat completions API.
type OpenAIProvider struct {
	profile ProviderProfile
	client  httpDoer
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

	logging.Debugf("openai call start profile=%s model=%s", profile.ID, model)
	resp, err := client.Do(httpReq)
	if err != nil {
		logging.Errorf("openai call error profile=%s err=%v", profile.ID, err)
		return ProviderResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		err := fmt.Errorf("openai api error: %s", resp.Status)
		logging.Errorf("openai call failed profile=%s err=%v", profile.ID, err)
		return ProviderResponse{}, err
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
    logging.Debugf("openai call success profile=%s model=%s", profile.ID, model)
    return ProviderResponse{Output: text, Metadata: meta, Chunks: buildChunksFromText(text)}, nil
}
