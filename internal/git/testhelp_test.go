package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	setup := [][]string{
		{"init", "-b", "master"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "komit test"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range setup {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return &Repo{Dir: dir}
}

func write(t *testing.T, r *Repo, path, content string) {
	t.Helper()
	full := filepath.Join(r.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitDo(t *testing.T, r *Repo, args ...string) string {
	t.Helper()
	out, err := r.run(args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// commitAll stages everything and commits, giving the repo a HEAD.
func commitAll(t *testing.T, r *Repo, msg string) {
	t.Helper()
	gitDo(t, r, "add", "-A")
	gitDo(t, r, "commit", "-m", msg)
}
