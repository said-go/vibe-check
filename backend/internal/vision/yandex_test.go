package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYandexAnalyzeRequestAndResult(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xd9}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Api-Key test-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("OpenAI-Project"); got != "folder-id" {
			t.Errorf("project = %q", got)
		}
		var body struct {
			Model               string  `json:"model"`
			Store               *bool   `json:"store"`
			MaxCompletionTokens int     `json:"max_completion_tokens"`
			ReasoningEffort     string  `json:"reasoning_effort"`
			Temperature         float64 `json:"temperature"`
			Messages            []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Model != "gpt://folder-id/qwen3.6-35b-a3b" {
			t.Errorf("model = %q", body.Model)
		}
		if body.Store == nil || *body.Store {
			t.Errorf("Yandex request must explicitly disable storage: store=%v", body.Store)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || len(body.Messages[0].Content) != 2 {
			t.Errorf("messages = %+v", body.Messages)
		} else {
			content := body.Messages[0].Content
			if content[0].Type != "text" || !strings.Contains(content[0].Text, "одежду") {
				t.Errorf("prompt = %+v", content[0])
			}
			want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
			if content[1].Type != "image_url" || content[1].ImageURL.URL != want {
				t.Errorf("image = %+v", content[1])
			}
		}
		if body.ResponseFormat.Type != "json_object" || body.MaxCompletionTokens != 4096 || body.ReasoningEffort != "none" || body.Temperature != 0.2 {
			t.Errorf("format or generation settings = %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": featureJSON}}}})
	}))
	defer server.Close()
	provider := NewYandex("test-key", "folder-id", "", server.URL, server.Client())
	features, err := provider.Analyze(context.Background(), jpeg)
	if err != nil || features.Confidence != 86 {
		t.Fatalf("features = %+v, error = %v", features, err)
	}
}

func TestYandexAnalyzeRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
		want error
	}{
		{"rate limit", 429, `{}`, ErrUnavailable},
		{"server error", 503, `{}`, ErrUnavailable},
		{"bad key", 401, `{}`, ErrUnavailable},
		{"missing permission", 403, `{}`, ErrUnavailable},
		{"missing model", 404, `{}`, ErrUnavailable},
		{"refusal", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","refusal":"cannot comply"}}]}`, ErrRejected},
		{"filtered", 200, `{"choices":[{"finish_reason":"content_filter","message":{"role":"assistant"}}]}`, ErrRejected},
		{"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"{}"}}]}`, ErrInvalidResponse},
		{"no choices", 200, `{"choices":[]}`, ErrInvalidResponse},
		{"invalid feature", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}]}`, ErrInvalidResponse},
		{"malformed envelope", 200, `{`, ErrInvalidResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			provider := NewYandex("key", "folder", "model", server.URL, server.Client())
			if _, err := provider.Analyze(context.Background(), []byte("jpeg")); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestYandexAnalyzeUnconfigured(t *testing.T) {
	provider := NewYandex("", "folder", "", "", http.DefaultClient)
	if _, err := provider.Analyze(context.Background(), []byte("jpeg")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestYandexAnalyzeRetriesInvalidModelAnswerOnce(t *testing.T) {
	var calls int
	var firstRequest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if calls == 1 {
			firstRequest = string(body)
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}]}`))
			return
		}
		if string(body) != firstRequest {
			t.Error("retry changed the image or prompt")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": featureJSON}}}})
	}))
	defer server.Close()
	provider := NewYandex("key", "folder", "model", server.URL, server.Client())
	features, err := provider.Analyze(context.Background(), []byte("jpeg"))
	if err != nil || features.Confidence != 86 || calls != 2 {
		t.Fatalf("features=%+v err=%v calls=%d", features, err, calls)
	}
}

func TestYandexAnalyzeDoesNotRetryRejectedOrUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want error
	}{
		{"refusal", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","refusal":"cannot comply"}}]}`, ErrRejected},
		{"bad request", 400, `{}`, ErrRejected},
		{"rate limited", 429, `{}`, ErrUnavailable},
		{"server unavailable", 503, `{}`, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			provider := NewYandex("key", "folder", "model", server.URL, server.Client())
			_, err := provider.Analyze(context.Background(), []byte("jpeg"))
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestYandexAnalyzeInvalidTwiceStops(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	provider := NewYandex("key", "folder", "model", server.URL, server.Client())
	_, err := provider.Analyze(context.Background(), []byte("jpeg"))
	if !errors.Is(err, ErrInvalidResponse) || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
