package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/said-go/vibe-check/backend/internal/config"
	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

func main() { os.Exit(run()) }
func run() int {
	input := flag.String("input", "", "local image manifest")
	allow := flag.Bool("allow-paid", false, "consent to sending manifest photos to configured AI and paid calls")
	max := flag.Int("max-calls", 10, "maximum HTTP AI attempts including retries (1..100)")
	flag.Parse()
	fail := func(s string) int { fmt.Fprintln(os.Stderr, s); return 2 }
	if !*allow {
		return fail("No images sent. Explicit -allow-paid is required.")
	}
	if *input == "" || flag.NArg() != 0 || *max < 1 || *max > 100 {
		return fail("Use -input manifest.json -allow-paid -max-calls 1..100")
	}
	f, e := os.Open(*input)
	if e != nil {
		return fail("Cannot open manifest")
	}
	defer f.Close()
	m, e := evaluation.DecodeImages(f)
	if e != nil {
		return fail(e.Error())
	}
	if len(m.Cases) > *max {
		return fail("Call budget is smaller than case count")
	}
	cfg, e := config.Load()
	if e != nil {
		return fail("Invalid environment configuration")
	}
	var p vision.Provider
	if cfg.AIProvider == "yandex" && cfg.YandexAPIKey != "" && cfg.YandexFolderID != "" {
		p = vision.NewYandex(cfg.YandexAPIKey, cfg.YandexFolderID, cfg.YandexModel, cfg.YandexBaseURL, &http.Client{Timeout: cfg.YandexTimeout})
	}
	if cfg.AIProvider == "openai" && cfg.OpenAIAPIKey != "" && cfg.OpenAIModel != "" {
		p = vision.NewOpenAI(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL, &http.Client{Timeout: cfg.OpenAITimeout})
	}
	if p == nil {
		return fail("Configure AI credentials in process environment; no preset fallback is used")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	dataset, e := evaluation.Collect(ctx, m, filepath.Dir(*input), p, *max)
	if e != nil {
		return fail(e.Error())
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(dataset) != nil {
		return fail("Cannot write dataset")
	}
	return 0
}
