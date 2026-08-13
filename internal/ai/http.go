package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// requestTimeout is a backstop for a server that accepts the request and then
// never answers; cancelling with esc is the ordinary way out.
const requestTimeout = 2 * time.Minute

// maxErrorBytes caps how much of an unrecognised error body reaches the status
// line, so an HTML error page cannot fill the screen.
const maxErrorBytes = 300

// message is one turn of a chat request. Both APIs spell it the same way.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: requestTimeout}
}

// postJSON sends body to url with the given headers and returns the response
// body, turning a non-2xx status into the provider's own error text.
func postJSON(ctx context.Context, client *http.Client, name, url string, headers map[string]string, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, wrap(ctx, name, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, wrap(ctx, name, err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s", name, apiError(data, resp.Status))
	}
	return data, nil
}

// wrap prefers the context's own error, so cancelling reads as cancelled rather
// than as a transport failure.
func wrap(ctx context.Context, name string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%s: %w", name, err)
}

// apiError digs the message out of the {"error":{"message":...}} envelope both
// APIs share, falling back to the raw body and then the HTTP status.
func apiError(data []byte, status string) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	if body := strings.TrimSpace(string(data)); body != "" {
		if len(body) > maxErrorBytes {
			body = body[:maxErrorBytes] + "…"
		}
		return body
	}
	return status
}
