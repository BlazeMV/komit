package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// anthropicVersion is the API version header every request must carry.
const anthropicVersion = "2023-06-01"

// maxTokens caps the completion. A commit message is short; this is a backstop
// against a model that will not stop on its own.
const maxTokens = 1024

// Anthropic calls the Messages API with an API key, for users without the CLI.
type Anthropic struct {
	Model   string
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func (a Anthropic) Run(ctx context.Context, prompt string) (string, error) {
	headers := map[string]string{"anthropic-version": anthropicVersion}
	if a.APIKey != "" {
		headers["x-api-key"] = a.APIKey
	}

	data, err := postJSON(ctx, a.HTTP, "anthropic", a.BaseURL+"/v1/messages", headers, map[string]any{
		"model":      a.Model,
		"max_tokens": maxTokens,
		"messages":   []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}

	var b strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	if b.Len() == 0 {
		return "", errors.New("anthropic: response carried no text")
	}
	return b.String(), nil
}
