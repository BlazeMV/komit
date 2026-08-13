package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BlazeMV/komit/internal/config"
)

// capture records what a runner sent, so a test can assert on the wire format
// without reaching the network.
type capture struct {
	path   string
	header http.Header
	body   map[string]any
}

// apiServer answers every request with reply and records the last one.
func apiServer(t *testing.T, status int, reply string) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		_ = json.Unmarshal(body, &got.body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestAnthropicSendsPromptAndReturnsText(t *testing.T) {
	srv, got := apiServer(t, 200, `{"content":[{"type":"text","text":"feat: from api"}]}`)

	out, err := Anthropic{Model: "claude-haiku-4-5", BaseURL: srv.URL, APIKey: "sk-ant", HTTP: srv.Client()}.
		Run(context.Background(), "the prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "feat: from api" {
		t.Errorf("Run = %q", out)
	}
	if got.path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", got.path)
	}
	if got.header.Get("x-api-key") != "sk-ant" {
		t.Errorf("x-api-key = %q", got.header.Get("x-api-key"))
	}
	if got.header.Get("anthropic-version") != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", got.header.Get("anthropic-version"), anthropicVersion)
	}
	if got.body["model"] != "claude-haiku-4-5" {
		t.Errorf("model = %v", got.body["model"])
	}
	if msg := firstMessage(t, got.body); msg != "the prompt" {
		t.Errorf("prompt = %q", msg)
	}
}

// A response can carry several blocks; only the text ones form the message.
func TestAnthropicJoinsTextBlocksAndSkipsOthers(t *testing.T) {
	srv, _ := apiServer(t, 200, `{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"a"},{"type":"text","text":"b"}]}`)

	out, err := Anthropic{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}.Run(context.Background(), "p")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ab" {
		t.Errorf("Run = %q, want the text blocks joined", out)
	}
}

func TestAnthropicSurfacesAPIError(t *testing.T) {
	srv, _ := apiServer(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)

	_, err := Anthropic{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}.Run(context.Background(), "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("err = %v, want the API's own message", err)
	}
}

func TestAnthropicEmptyContentIsAnError(t *testing.T) {
	srv, _ := apiServer(t, 200, `{"content":[]}`)

	if _, err := (Anthropic{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}).Run(context.Background(), "p"); err == nil {
		t.Fatal("expected an error for a response with no text")
	}
}

func TestOpenAISendsPromptAndReturnsContent(t *testing.T) {
	srv, got := apiServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"feat: from openai"}}]}`)

	out, err := OpenAI{Model: "gpt-5-mini", BaseURL: srv.URL, APIKey: "sk-oa", HTTP: srv.Client()}.
		Run(context.Background(), "the prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "feat: from openai" {
		t.Errorf("Run = %q", out)
	}
	if got.path != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", got.path)
	}
	if got.header.Get("authorization") != "Bearer sk-oa" {
		t.Errorf("authorization = %q", got.header.Get("authorization"))
	}
	if msg := firstMessage(t, got.body); msg != "the prompt" {
		t.Errorf("prompt = %q", msg)
	}
}

// Reasoning models reject max_tokens, so the request must not carry a cap.
func TestOpenAISendsNoTokenCap(t *testing.T) {
	srv, got := apiServer(t, 200, `{"choices":[{"message":{"content":"x"}}]}`)

	if _, err := (OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}).Run(context.Background(), "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := got.body["max_tokens"]; ok {
		t.Error("request carried max_tokens")
	}
}

// Ollama and LM Studio want no credentials at all.
func TestOpenAIOmitsAuthorizationWithoutAKey(t *testing.T) {
	srv, got := apiServer(t, 200, `{"choices":[{"message":{"content":"x"}}]}`)

	if _, err := (OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}).Run(context.Background(), "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := got.header["Authorization"]; ok {
		t.Error("request carried an Authorization header with no key configured")
	}
}

func TestOpenAISurfacesAPIError(t *testing.T) {
	srv, _ := apiServer(t, 429, `{"error":{"message":"rate limit reached","type":"rate_limit_error"}}`)

	_, err := OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}.Run(context.Background(), "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rate limit reached") {
		t.Errorf("err = %v, want the API's own message", err)
	}
}

func TestOpenAINoChoicesIsAnError(t *testing.T) {
	srv, _ := apiServer(t, 200, `{"choices":[]}`)

	if _, err := (OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}).Run(context.Background(), "p"); err == nil {
		t.Fatal("expected an error for a response with no choices")
	}
}

// A body that is not the shared error envelope still has to say something.
func TestAPIErrorFallsBackToTheBody(t *testing.T) {
	srv, _ := apiServer(t, 502, "<html>bad gateway</html>")

	_, err := OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()}.Run(context.Background(), "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "bad gateway") {
		t.Errorf("err = %v, want the body echoed", err)
	}
}

func TestAPIErrorTruncatesAHugeBody(t *testing.T) {
	got := apiError([]byte(strings.Repeat("x", 5000)), "500 Internal Server Error")
	if len(got) > maxErrorBytes+len("…") {
		t.Errorf("message is %d bytes, want it capped near %d", len(got), maxErrorBytes)
	}
}

func TestRunnersRespectContextCancellation(t *testing.T) {
	// The handler needs its own release: a client-side cancel does not reliably
	// cancel the server's request context, and Close waits for live handlers.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	runners := map[string]Runner{
		"anthropic": Anthropic{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()},
		"openai":    OpenAI{Model: "m", BaseURL: srv.URL, HTTP: srv.Client()},
	}
	for name, r := range runners {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			start := time.Now()
			if _, err := r.Run(ctx, "p"); err == nil {
				t.Fatal("expected an error when the context expires")
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("Run took %s, want it to stop with the context", elapsed)
			}
		})
	}
}

func TestNewBuildsTheConfiguredProvider(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(k, "")
	}
	tests := []struct {
		provider string
		want     Runner
	}{
		{config.ProviderCLI, CLI{Bin: config.DefaultBin, Model: "haiku"}},
		{config.ProviderAnthropic, Anthropic{Model: "claude-haiku-4-5", BaseURL: config.AnthropicBaseURL, APIKey: "sk-x"}},
		{config.ProviderOpenAI, OpenAI{Model: "gpt-5.6-luna", BaseURL: config.OpenAIBaseURL, APIKey: "sk-x"}},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			cfg := config.Default()
			cfg.Provider = tt.provider
			p := cfg.Providers[tt.provider]
			p.APIKey = "sk-x"
			cfg.Providers[tt.provider] = p

			got, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			switch r := got.(type) {
			case CLI:
				if r != tt.want {
					t.Errorf("New = %+v, want %+v", r, tt.want)
				}
			case Anthropic:
				want := tt.want.(Anthropic)
				if r.Model != want.Model || r.BaseURL != want.BaseURL || r.APIKey != want.APIKey {
					t.Errorf("New = %+v, want %+v", r, want)
				}
				if r.HTTP == nil {
					t.Error("New left the HTTP client nil")
				}
			case OpenAI:
				want := tt.want.(OpenAI)
				if r.Model != want.Model || r.BaseURL != want.BaseURL || r.APIKey != want.APIKey {
					t.Errorf("New = %+v, want %+v", r, want)
				}
				if r.HTTP == nil {
					t.Error("New left the HTTP client nil")
				}
			default:
				t.Fatalf("New returned %T", got)
			}
		})
	}
}

func TestNewRejectsAnUnknownProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = "gemini"

	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
}

// firstMessage digs the user turn out of a recorded request body.
func firstMessage(t *testing.T, body map[string]any) string {
	t.Helper()
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("request carried no messages: %v", body)
	}
	first, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message is not an object: %v", messages[0])
	}
	if role := first["role"]; role != "user" {
		t.Errorf("role = %v, want user", role)
	}
	content, _ := first["content"].(string)
	return content
}

// A labelled block builds the runner its type names, not its label.
func TestNewFollowsTheBlockType(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = "ollama"
	cfg.Providers["ollama"] = config.Provider{
		Type:    config.ProviderOpenAI,
		Model:   "qwen3",
		BaseURL: "http://localhost:11434/v1",
	}

	got, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r, ok := got.(OpenAI)
	if !ok {
		t.Fatalf("New returned %T, want OpenAI", got)
	}
	if r.Model != "qwen3" || r.BaseURL != "http://localhost:11434/v1" || r.APIKey != "" {
		t.Errorf("New = %+v", r)
	}
}
