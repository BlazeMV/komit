package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// OpenAI calls any OpenAI-compatible chat completions endpoint: OpenAI itself,
// OpenRouter, Groq, Ollama, LM Studio. No token cap is sent — the reasoning
// models reject max_tokens, and the prompt already asks for one line.
// ReasoningEffort is opt-in for that same reason — an endpoint that does not
// know the field rejects the request outright.
type OpenAI struct {
	Model           string
	BaseURL         string
	APIKey          string
	ReasoningEffort string
	HTTP            *http.Client
}

func (o OpenAI) Run(ctx context.Context, prompt string) (string, error) {
	headers := map[string]string{}
	if o.APIKey != "" {
		headers["authorization"] = "Bearer " + o.APIKey
	}

	body := map[string]any{
		"model":    o.Model,
		"messages": []message{{Role: "user", Content: prompt}},
	}
	if o.ReasoningEffort != "" {
		body["reasoning_effort"] = o.ReasoningEffort
	}

	data, err := postJSON(ctx, o.HTTP, "openai", o.BaseURL+"/chat/completions", headers, body)
	if err != nil {
		return "", err
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("openai: response carried no message")
	}
	return out.Choices[0].Message.Content, nil
}
