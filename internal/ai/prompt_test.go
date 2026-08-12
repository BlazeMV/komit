package ai

import (
	"strings"
	"testing"
)

func TestTruncateDiffLeavesSmallDiffAlone(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-one\n+two\n"
	if got := TruncateDiff(diff); got != diff {
		t.Errorf("small diff was modified:\n%s", got)
	}
}

func TestTruncateDiffCapsPerFile(t *testing.T) {
	big := strings.Repeat("+line of a very long generated file\n", 2000) // ~72KB
	diff := "diff --git a/small.go b/small.go\n+kept\n" +
		"diff --git a/big.go b/big.go\n" + big

	got := TruncateDiff(diff)
	if !strings.Contains(got, "+kept") {
		t.Error("truncation dropped the small file")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("no truncation marker in output")
	}
	if len(got) > MaxDiffBytes {
		t.Errorf("output is %d bytes, want <= %d", len(got), MaxDiffBytes)
	}
	if !strings.Contains(got, "big.go") {
		t.Error("truncated file lost its header")
	}
}

func TestTruncateDiffCapsWhole(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("diff --git a/f.go b/f.go\n")
		b.WriteString(strings.Repeat("+x\n", 500))
	}
	got := TruncateDiff(b.String())
	if len(got) > MaxDiffBytes {
		t.Errorf("output is %d bytes, want <= %d", len(got), MaxDiffBytes)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("no truncation marker in output")
	}
}

func TestClean(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "feat: add thing", "feat: add thing"},
		{"trailing newline", "feat: add thing\n\n", "feat: add thing"},
		{"fenced", "```\nfeat: add thing\n```", "feat: add thing"},
		{"fenced with language", "```text\nfeat: add thing\n```\n", "feat: add thing"},
		{"body preserved", "feat: add thing\n\nbecause reasons", "feat: add thing\n\nbecause reasons"},
		{"inner backticks kept", "fix: escape `--only` correctly", "fix: escape `--only` correctly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
