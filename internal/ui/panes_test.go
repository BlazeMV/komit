package ui

import (
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
)

func TestDiffPaneTogglesAndShowsContent(t *testing.T) {
	m := modelWithFiles()
	if strings.Contains(m.View().Content, "@@") {
		t.Fatal("diff pane visible before it was requested")
	}

	m = update(m, key("d"))
	m = update(m, diffMsg{path: "a.go", body: "@@ -1 +1 @@\n-old\n+new\n"})

	out := m.View().Content
	if !strings.Contains(out, "+new") {
		t.Errorf("diff pane missing content:\n%s", out)
	}

	m = update(m, key("d"))
	if strings.Contains(m.View().Content, "+new") {
		t.Error("diff pane did not hide")
	}
}

func TestDiffLoadsForCursorFileNotSelection(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, key("down")) // cursor on b.go
	m = update(m, diffMsg{path: "b.go", body: "@@ b @@"})

	if !strings.Contains(m.View().Content, "b.go") {
		t.Error("diff pane header does not name the cursor file")
	}
}

func TestMessageEditorAcceptsTyping(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("e"))
	for _, r := range "fix: thing" {
		m = update(m, key(string(r)))
	}
	if got := m.message(); got != "fix: thing" {
		t.Errorf("message = %q, want %q", got, "fix: thing")
	}
	if !strings.Contains(m.View().Content, "fix: thing") {
		t.Error("view does not show the typed message")
	}
}

func TestEscapeLeavesEditorAndKeysBindAgain(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("e"))
	m = update(m, key("a")) // typed, not select-all
	m = update(m, key("esc"))
	m = update(m, key("a")) // bound to toggleAll again, not typed

	if m.message() != "a" {
		t.Errorf("message = %q, want the typed 'a'", m.message())
	}
	// modelWithFiles() starts fully selected, so toggleAll clears it.
	if len(m.selectedPaths()) != 0 {
		t.Errorf("toggleAll did not run after esc: %v", m.selectedPaths())
	}
}

func TestGeneratedMsgFillsEditor(t *testing.T) {
	m := modelWithFiles()
	m = update(m, generatedMsg{message: "feat: generated"})
	if m.message() != "feat: generated" {
		t.Errorf("message = %q", m.message())
	}
	if m.busy {
		t.Error("busy flag still set after generation finished")
	}
}

func TestHeaderShowsActiveProviderAndModel(t *testing.T) {
	m := modelWithFiles()
	m.cfg = config.Config{
		Provider: "openrouter",
		Providers: map[string]config.Provider{
			"anthropic":  {Type: config.ProviderAnthropic, Model: "claude-opus-5"},
			"openrouter": {Type: config.ProviderOpenAI, Model: "glm-4.6"},
		},
	}

	out := m.View().Content
	if !strings.Contains(out, "openrouter · glm-4.6") {
		t.Errorf("header does not name the active provider and model:\n%s", out)
	}
	if strings.Contains(out, "claude-opus-5") {
		t.Errorf("header names an inactive provider's model:\n%s", out)
	}
}

func TestProviderLineOmitsModelWhenBlockHasNone(t *testing.T) {
	m := Model{cfg: config.Config{
		Provider:  "local",
		Providers: map[string]config.Provider{"local": {Type: config.ProviderOpenAI}},
	}}
	if got := m.providerLine(); got != "local" {
		t.Errorf("providerLine() = %q, want %q", got, "local")
	}
}

func TestHeaderOmitsProviderWhenUnconfigured(t *testing.T) {
	header, _, _ := strings.Cut(modelWithFiles().View().Content, "\n")
	if strings.Count(header, "·") != 1 {
		t.Errorf("header has a dangling separator with no provider configured: %q", header)
	}
}

func TestPartialStagingWarningShownForSelectedFile(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = update(m, statusMsg{files: []git.FileChange{
		{Path: "partial.go", Index: 'M', Worktree: 'M'},
	}})
	if !strings.Contains(m.View().Content, "±") {
		t.Error("no partial-staging marker")
	}
}
