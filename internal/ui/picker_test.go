package ui

import (
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/ai"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
)

// pickerConfig has three blocks in deliberate non-alphabetical declaration
// order: two usable, one missing its key.
func pickerConfig() config.Config {
	return config.Config{
		Provider: "openrouter",
		Providers: map[string]config.Provider{
			"openrouter": {Type: config.ProviderOpenAI, Model: "glm-4.6", BaseURL: "https://openrouter.test/v1", APIKey: "sk-or"},
			"local":      {Type: config.ProviderOpenAI, Model: "qwen3", BaseURL: "http://localhost:11434/v1"},
			"anthropic":  {Type: config.ProviderAnthropic, Model: "claude-opus-5"},
		},
		Prompt: "{{diff}}",
	}
}

func pickerModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	m := New(newUIRepo(t), pickerConfig(), &fakeRunner{}, nil)
	m.width, m.height = 100, 30
	return update(m, statusMsg{
		files:  []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
		branch: git.Branch{Name: "master"},
	})
}

func TestProviderPickerListsEveryBlock(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	if !m.picking {
		t.Fatal("p did not open the picker")
	}

	out := m.View().Content
	for _, want := range []string{"anthropic", "local", "openrouter", "qwen3", "glm-4.6", config.ProviderAnthropic} {
		if !strings.Contains(out, want) {
			t.Errorf("picker does not show %q:\n%s", want, out)
		}
	}
}

// Opening on any other row would make enter a trap: the obvious keypress would
// silently switch away from what you are using.
func TestProviderPickerOpensOnTheActiveBlock(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	if got := m.pickRows[m.pickCursor].label; got != "openrouter" {
		t.Errorf("cursor opened on %q, want the active block", got)
	}
}

func TestProviderPickerNamesTheProblemWithAnUnusableBlock(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	if !strings.Contains(m.View().Content, "needs an API key") {
		t.Errorf("picker does not flag the block with no key:\n%s", m.View().Content)
	}
}

func TestPickingAProviderSwitchesConfigAndRunner(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	m = update(m, key(keyUp)) // sorted: anthropic, local, openrouter
	m = update(m, key("enter"))

	if m.picking {
		t.Error("picker stayed open after a successful pick")
	}
	if m.cfg.Provider != "local" {
		t.Fatalf("cfg.Provider = %q, want local", m.cfg.Provider)
	}
	if _, stale := m.runner.(*fakeRunner); stale {
		t.Error("runner was not rebuilt for the new provider")
	}
	if r, ok := m.runner.(ai.OpenAI); !ok || r.Model != "qwen3" {
		t.Errorf("runner = %#v, want an ai.OpenAI on qwen3", m.runner)
	}
	if !strings.Contains(m.View().Content, "local · qwen3") {
		t.Errorf("header does not show the new provider:\n%s", m.View().Content)
	}
}

func TestPickingAnUnusableProviderIsRefused(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	m = update(m, key(keyUp))
	m = update(m, key(keyUp)) // anthropic, which has no key
	m = update(m, key("enter"))

	if m.cfg.Provider != "openrouter" {
		t.Errorf("cfg.Provider = %q, want the switch refused", m.cfg.Provider)
	}
	if _, kept := m.runner.(*fakeRunner); !kept {
		t.Errorf("runner = %#v, want the original kept", m.runner)
	}
	if !m.picking {
		t.Error("picker closed on a refused pick, giving no chance to choose another")
	}
	if !strings.Contains(m.View().Content, "needs an API key") {
		t.Errorf("refusal does not say why:\n%s", m.View().Content)
	}
}

func TestProviderPickerEscapeKeepsTheCurrentProvider(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	m = update(m, key(keyUp))
	m = update(m, key(keyCancel))

	if m.picking {
		t.Error("esc did not close the picker")
	}
	if m.cfg.Provider != "openrouter" {
		t.Errorf("cfg.Provider = %q, want esc to change nothing", m.cfg.Provider)
	}
	if _, kept := m.runner.(*fakeRunner); !kept {
		t.Errorf("runner = %#v, want the original kept", m.runner)
	}
}

// Every other action key no-ops while a generation is running; the picker must
// too, or the in-flight run's provider stops matching the header.
func TestProviderPickerDoesNotOpenWhileBusy(t *testing.T) {
	m := pickerModel(t)
	m.busy = true
	if update(m, key(keyProvider)).picking {
		t.Error("picker opened during a generation")
	}
}

// While picking, runes are the picker's: they must not reach the file list.
func TestProviderPickerSwallowsFileKeys(t *testing.T) {
	m := update(pickerModel(t), key(keyProvider))
	before := len(m.selectedPaths())
	m = update(m, key(keyToggleAll))

	if got := len(m.selectedPaths()); got != before {
		t.Errorf("selected %d paths, want %d — 'a' leaked to the file list", got, before)
	}
}
