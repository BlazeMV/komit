package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlazeMV/komit/internal/config"
)

// fakeClaude installs a script named "claude" on PATH. body is shell that can
// read the prompt from stdin and write to stdout; args land in "$@".
func fakeClaude(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLIPassesPromptOnStdinAndReturnsStdout(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "feat: from fake"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "the prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "feat: from fake" {
		t.Errorf("Run = %q", got)
	}
}

func TestCLIPromptReachesStdin(t *testing.T) {
	fakeClaude(t, `read -r line; echo "got:$line"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "hello prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "got:hello prompt" {
		t.Errorf("Run = %q, want the prompt echoed back", got)
	}
}

func TestCLIPassesExpectedFlags(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "$@"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "p")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"-p", "--model haiku", "--output-format text", "--safe-mode", "--no-session-persistence"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", strings.TrimSpace(got), want)
		}
	}
}

func TestCLIMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := CLI{}.Run(context.Background(), "haiku", "p")
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("err = %v, want ErrMissing", err)
	}
}

func TestCLIFailureIncludesStderr(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "credit balance too low" >&2; exit 1`)

	_, err := CLI{}.Run(context.Background(), "haiku", "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "credit balance too low") {
		t.Errorf("err = %v, want claude's stderr", err)
	}
}

func TestCLIRespectsContextCancellation(t *testing.T) {
	// exec replaces the shell so a killed direct child cannot leave sleep
	// running as an orphaned grandchild.
	fakeClaude(t, `cat > /dev/null; exec sleep 30`)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := (CLI{}).Run(ctx, "haiku", "p"); err == nil {
		t.Fatal("expected an error when the context expires")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Run took %s, want it to stop with the context", elapsed)
	}
}

type stubRunner struct {
	gotModel  string
	gotPrompt string
	out       string
}

func (s *stubRunner) Run(_ context.Context, model, prompt string) (string, error) {
	s.gotModel, s.gotPrompt = model, prompt
	return s.out, nil
}

func TestGenerateRendersPromptAndCleansOutput(t *testing.T) {
	stub := &stubRunner{out: "```\nfeat: add thing\n```"}
	cfg := config.Config{Model: "haiku", Prompt: "files={{files}} diff={{diff}}"}

	got, err := Generate(context.Background(), stub, cfg, config.Vars{
		Diff:  "THEDIFF",
		Files: "a.go",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "feat: add thing" {
		t.Errorf("Generate = %q", got)
	}
	if stub.gotModel != "haiku" {
		t.Errorf("model = %q", stub.gotModel)
	}
	if stub.gotPrompt != "files=a.go diff=THEDIFF" {
		t.Errorf("prompt = %q", stub.gotPrompt)
	}
}

func TestGenerateTruncatesDiff(t *testing.T) {
	stub := &stubRunner{out: "msg"}
	cfg := config.Config{Model: "haiku", Prompt: "{{diff}}"}

	_, err := Generate(context.Background(), stub, cfg, config.Vars{
		Diff: strings.Repeat("+x\n", 100_000),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(stub.gotPrompt) > MaxDiffBytes+1024 {
		t.Errorf("prompt is %d bytes, want the diff truncated", len(stub.gotPrompt))
	}
}
