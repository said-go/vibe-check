package vision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const featureJSON = `{"contract_version":"1.0.0","dominant_colors":["black","gray"],"color_harmony":82,"darkness":76,"formality":68,"sportiness":41,"originality":73,"outfit_coherence":88,"accessory_intensity":57,"layering":44,"traditional_elements":12,"image_quality":91,"outfit_visibility":90,"confidence":86,"person_count":1,"subculture_signals":{"goth":38,"alternative":22,"streetwear":46,"classic":75,"sport_chic":51}}`

func TestOpenAIAnalyzeUsesImageAndStrictSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		serialized, _ := json.Marshal(body)
		text := string(serialized)
		if !strings.Contains(text, "data:image/jpeg;base64,") {
			t.Error("image data URL missing")
		}
		if !strings.Contains(text, `"strict":true`) {
			t.Error("strict schema missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": featureJSON}}}}})
	}))
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	provider := NewOpenAI("test-key", "vision-model", server.URL, client)
	features, err := provider.Analyze(context.Background(), []byte{0xff, 0xd8, 0xff, 0xd9})
	if err != nil {
		t.Fatal(err)
	}
	if features.Confidence != 86 || features.Signals.Classic != 75 {
		t.Fatalf("unexpected features: %+v", features)
	}
}

func TestOpenAIAnalyzeHandlesRefusalAndTemporaryFailure(t *testing.T) {
	refusal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "refusal", "refusal": "cannot process"}}}}})
	}))
	defer refusal.Close()
	provider := NewOpenAI("key", "model", refusal.URL, refusal.Client())
	if _, err := provider.Analyze(context.Background(), []byte("jpeg")); !errors.Is(err, ErrRejected) {
		t.Fatalf("expected rejected, got %v", err)
	}

	temporary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer temporary.Close()
	provider = NewOpenAI("key", "model", temporary.URL, temporary.Client())
	if _, err := provider.Analyze(context.Background(), []byte("jpeg")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
}
