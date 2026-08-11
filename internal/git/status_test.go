package git

import "testing"

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []FileChange
	}{
		{
			name: "staged modification",
			in:   "M  cmd/main.go\x00",
			want: []FileChange{{Index: 'M', Worktree: ' ', Path: "cmd/main.go"}},
		},
		{
			name: "unstaged modification",
			in:   " M cmd/main.go\x00",
			want: []FileChange{{Index: ' ', Worktree: 'M', Path: "cmd/main.go"}},
		},
		{
			name: "untracked",
			in:   "?? new.go\x00",
			want: []FileChange{{Index: '?', Worktree: '?', Path: "new.go"}},
		},
		{
			name: "rename carries original path",
			in:   "R  new.go\x00old.go\x00",
			want: []FileChange{{Index: 'R', Worktree: ' ', Path: "new.go", Orig: "old.go"}},
		},
		{
			name: "path with spaces is not split",
			in:   " M a file with spaces.go\x00",
			want: []FileChange{{Index: ' ', Worktree: 'M', Path: "a file with spaces.go"}},
		},
		{
			name: "multiple entries",
			in:   "M  a.go\x00?? b.go\x00",
			want: []FileChange{
				{Index: 'M', Worktree: ' ', Path: "a.go"},
				{Index: '?', Worktree: '?', Path: "b.go"},
			},
		},
		{name: "empty output", in: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatus(tt.in)
			if err != nil {
				t.Fatalf("ParseStatus(%q) error: %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d changes, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseStatusRenameMissingOriginal(t *testing.T) {
	if _, err := ParseStatus("R  new.go\x00"); err == nil {
		t.Fatal("expected error for rename entry with no original path")
	}
}

func TestFileChangeClassification(t *testing.T) {
	tests := []struct {
		name               string
		c                  FileChange
		untracked, partial bool
		letter             string
	}{
		{"untracked", FileChange{Index: '?', Worktree: '?'}, true, false, "?"},
		{"staged only", FileChange{Index: 'M', Worktree: ' '}, false, false, "M"},
		{"unstaged only", FileChange{Index: ' ', Worktree: 'M'}, false, false, "M"},
		{"partially staged", FileChange{Index: 'M', Worktree: 'M'}, false, true, "M"},
		{"added", FileChange{Index: 'A', Worktree: ' '}, false, false, "A"},
		{"deleted in worktree", FileChange{Index: ' ', Worktree: 'D'}, false, false, "D"},
		{"renamed", FileChange{Index: 'R', Worktree: ' '}, false, false, "R"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Untracked(); got != tt.untracked {
				t.Errorf("Untracked() = %v, want %v", got, tt.untracked)
			}
			if got := tt.c.PartiallyStaged(); got != tt.partial {
				t.Errorf("PartiallyStaged() = %v, want %v", got, tt.partial)
			}
			if got := tt.c.Letter(); got != tt.letter {
				t.Errorf("Letter() = %q, want %q", got, tt.letter)
			}
		})
	}
}
