package ai

import (
	"context"

	"github.com/BlazeMV/komit/internal/config"
)

// Runner executes a prompt and returns the raw completion. The model is fixed
// when the runner is built, since which names are valid depends on the provider.
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Generate renders cfg.Prompt with vars (diff truncated first), runs it and
// cleans the result into a commit message.
func Generate(ctx context.Context, r Runner, cfg config.Config, v config.Vars) (string, error) {
	v.Diff = TruncateDiff(v.Diff)
	out, err := r.Run(ctx, config.Render(cfg.Prompt, v))
	if err != nil {
		return "", err
	}
	return Clean(out), nil
}
