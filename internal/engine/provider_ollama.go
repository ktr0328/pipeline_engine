package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/pipeline-engine/pkg/logging"
)

// OllamaProvider calls a local Ollama HTTP endpoint.
type OllamaProvider struct {
	profile ProviderProfile
	client  httpDoer
}

func (p *OllamaProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return callOllama(ctx, req, p.profile, p.httpClient())
}

func (p *OllamaProvider) httpClient() httpDoer {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type ollamaRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	System  string         `json:"system,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Model    string `json:"model"`
	Done     bool   `json:"done"`
}

func callOllama(ctx context.Context, req ProviderRequest, profile ProviderProfile, client httpDoer) (ProviderResponse, error) {
	model := profile.DefaultModel
	if model == "" {
		model = "llama3"
	}
	base := profile.BaseURI
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	url := strings.TrimRight(base, "/") + "/api/generate"

	prompt := req.Prompt
	reqPayload := ollamaRequest{Model: model, Prompt: prompt, Stream: false}
	if req.Profile.Extra != nil {
		if sys, ok := req.Profile.Extra["system_prompt"].(string); ok && sys != "" {
			reqPayload.System = sys
		}
		if opts, ok := req.Profile.Extra["options"].(map[string]any); ok && len(opts) > 0 {
			reqPayload.Options = opts
		}
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	logging.Debugf("ollama call start profile=%s model=%s", profile.ID, model)
	resp, err := client.Do(httpReq)
	if err != nil {
		logging.Errorf("ollama call error profile=%s err=%v", profile.ID, err)
		return ProviderResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		err := fmt.Errorf("ollama api error: %s", resp.Status)
		logging.Errorf("ollama call failed profile=%s err=%v", profile.ID, err)
		return ProviderResponse{}, err
	}

	var decoded ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ProviderResponse{}, err
	}
	if decoded.Response == "" {
		return ProviderResponse{}, errors.New("ollama response is empty")
	}
	modelName := decoded.Model
	if modelName == "" {
		modelName = model
	}
	meta := map[string]any{
		"provider": "ollama",
		"model":    modelName,
	}
    logging.Debugf("ollama call success profile=%s model=%s", profile.ID, modelName)
    return ProviderResponse{Output: decoded.Response, Metadata: meta, Chunks: buildChunksFromText(decoded.Response)}, nil
}
