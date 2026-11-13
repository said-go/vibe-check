package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

const (
	yandexDefaultEndpoint = "https://ai.api.cloud.yandex.net/v1"
	yandexDefaultModel    = "qwen3.6-35b-a3b"
)

// Yandex uses AI Studio's OpenAI-compatible Chat Completions API.
// See https://aistudio.yandex.ru/en/docs/ai-studio/operations/generation/multimodels-request
type Yandex struct {
	apiKey   string
	folderID string
	model    string
	endpoint string
	client   *http.Client
}

// NewYandex creates a vision provider. An empty model selects Yandex's documented
// multimodal example model; an empty endpoint selects the public AI Studio API.
func NewYandex(apiKey, folderID, model, endpoint string, client *http.Client) *Yandex {
	if model == "" {
		model = yandexDefaultModel
	}
	if endpoint == "" {
		endpoint = yandexDefaultEndpoint
	}
	return &Yandex{
		apiKey: apiKey, folderID: folderID, model: model,
		endpoint: strings.TrimRight(endpoint, "/"), client: client,
	}
}

type yandexContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type yandexRequest struct {
	Model               string  `json:"model"`
	Store               bool    `json:"store"`
	MaxCompletionTokens int     `json:"max_completion_tokens"`
	ReasoningEffort     string  `json:"reasoning_effort"`
	Temperature         float64 `json:"temperature"`
	Messages            []struct {
		Role    string          `json:"role"`
		Content []yandexContent `json:"content"`
	} `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type yandexResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
}

func (p *Yandex) Analyze(ctx context.Context, sanitizedJPEG []byte) (analysis.VisualFeatures, error) {
	if p == nil || p.apiKey == "" || p.folderID == "" || p.model == "" || p.client == nil {
		return analysis.VisualFeatures{}, ErrUnavailable
	}
	if len(sanitizedJPEG) == 0 {
		return analysis.VisualFeatures{}, ErrInvalidResponse
	}
	requestBody := yandexRequest{Model: "gpt://" + p.folderID + "/" + p.model}
	requestBody.MaxCompletionTokens = 1600
	requestBody.ReasoningEffort = "low"
	requestBody.Temperature = 0.2
	requestBody.Messages = append(requestBody.Messages, struct {
		Role    string          `json:"role"`
		Content []yandexContent `json:"content"`
	}{Role: "user", Content: []yandexContent{
		{Type: "text", Text: analysisPrompt},
		{Type: "image_url", ImageURL: &struct {
			URL string `json:"url"`
		}{URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(sanitizedJPEG)}},
	}})
	requestBody.ResponseFormat.Type = "json_object"
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("marshal Yandex request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("create Yandex request: %w", err)
	}
	request.Header.Set("Authorization", "Api-Key "+p.apiKey)
	request.Header.Set("OpenAI-Project", p.folderID)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: read Yandex response: %v", ErrUnavailable, err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: status %d", ErrRejected, response.StatusCode)
	}
	var envelope yandexResponse
	if err := json.Unmarshal(data, &envelope); err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if len(envelope.Choices) != 1 {
		return analysis.VisualFeatures{}, ErrInvalidResponse
	}
	choice := envelope.Choices[0]
	if choice.Message.Refusal != "" || choice.FinishReason == "content_filter" {
		return analysis.VisualFeatures{}, ErrRejected
	}
	if choice.FinishReason != "stop" || choice.Message.Role != "assistant" || strings.TrimSpace(choice.Message.Content) == "" {
		return analysis.VisualFeatures{}, ErrInvalidResponse
	}
	features, err := analysis.DecodeFeatures(strings.NewReader(choice.Message.Content))
	if err != nil {
		return analysis.VisualFeatures{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return features, nil
}
