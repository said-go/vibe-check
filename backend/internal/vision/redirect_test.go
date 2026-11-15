package vision

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProvidersDoNotRedirectImages(t *testing.T) {
	for _, kind := range []string{"yandex", "openai"} {
		for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			t.Run(fmt.Sprintf("%s/%d", kind, status), func(t *testing.T) {
				var targetCalls, sourceCalls atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					targetCalls.Add(1)
					w.WriteHeader(http.StatusForbidden)
				}))
				defer target.Close()
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sourceCalls.Add(1)
					http.Redirect(w, r, target.URL, status)
				}))
				defer source.Close()
				client := source.Client()
				var provider Provider
				if kind == "yandex" {
					provider = NewYandex("fake", "fake", "fake", source.URL, client)
				} else {
					provider = NewOpenAI("fake", "fake", source.URL, client)
				}
				attempts := 0
				ctx := WithAttemptGuard(context.Background(), func() error { attempts++; return nil })
				_, err := provider.Analyze(ctx, []byte("synthetic-private-image"))
				if !errors.Is(err, ErrRejected) || attempts != 1 || sourceCalls.Load() != 1 || targetCalls.Load() != 0 {
					t.Fatalf("err=%v attempts=%d source=%d target=%d", err, attempts, sourceCalls.Load(), targetCalls.Load())
				}
				// The original shared client still follows redirects normally.
				response, err := client.Post(source.URL, "text/plain", strings.NewReader("caller-test"))
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if targetCalls.Load() != 1 {
					t.Fatal("provider changed the caller's redirect policy")
				}
			})
		}
	}
}
