package ai

import (
	"fmt"

	"github.com/BlazeMV/komit/internal/config"
)

// New builds the runner for the configured provider. Run config.Validate first:
// New trusts that the settings it reads are present and well formed.
func New(cfg config.Config) (Runner, error) {
	p := cfg.Active()
	switch cfg.Provider {
	case config.ProviderCLI:
		return CLI{Bin: cfg.Bin(), Model: p.Model}, nil
	case config.ProviderAnthropic:
		return Anthropic{
			Model:   p.Model,
			BaseURL: cfg.BaseURL(),
			APIKey:  cfg.APIKey(),
			HTTP:    newHTTPClient(),
		}, nil
	case config.ProviderOpenAI:
		return OpenAI{
			Model:   p.Model,
			BaseURL: cfg.BaseURL(),
			APIKey:  cfg.APIKey(),
			HTTP:    newHTTPClient(),
		}, nil
	}
	return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
}
