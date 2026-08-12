package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrMissing means the claude CLI is not installed or not on PATH.
var ErrMissing = errors.New("claude CLI not found on PATH")

// CLI runs the claude binary in headless print mode. --safe-mode is load-bearing:
// without it the repo's CLAUDE.md, hooks and MCP servers change the output.
type CLI struct {
	Bin string
}

func (c CLI) Run(ctx context.Context, model, prompt string) (string, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", ErrMissing
	}

	cmd := exec.CommandContext(ctx, bin,
		"-p",
		"--model", model,
		"--output-format", "text",
		"--safe-mode",
		"--no-session-persistence",
	)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	return stdout.String(), nil
}
