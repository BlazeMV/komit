// Package ai turns a diff into a commit message using the claude CLI.
package ai

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// MaxDiffBytes caps the whole diff sent to the model.
	MaxDiffBytes = 60 << 10
	// MaxFileBytes caps any single file's diff before the whole-diff cap runs.
	MaxFileBytes = 20 << 10
)

// TruncateDiff keeps a diff under MaxDiffBytes: per-file cap first, then the
// tail. Every cut is marked — unmarked truncation reads as a complete diff.
func TruncateDiff(diff string) string {
	if len(diff) <= MaxDiffBytes {
		return diff
	}

	const sep = "diff --git "

	var b strings.Builder
	for i, part := range strings.Split(diff, sep) {
		if i == 0 {
			if part != "" {
				b.WriteString(capPart(part))
			}
			continue
		}
		b.WriteString(sep)
		b.WriteString(capPart(part))
	}

	out := b.String()
	if len(out) > MaxDiffBytes {
		// Ensure truncated result stays within MaxDiffBytes.
		omitted := len(out) - MaxDiffBytes + 50
		markerLen := 38 + len(strconv.Itoa(omitted))
		slicePos := MaxDiffBytes - markerLen
		omitted = len(out) - slicePos
		marker := fmt.Sprintf("\n... [diff truncated, %d bytes omitted]\n", omitted)
		out = out[:slicePos] + marker
	}
	return out
}

func capPart(part string) string {
	if len(part) <= MaxFileBytes {
		return part
	}
	return part[:MaxFileBytes] + fmt.Sprintf("\n... [truncated, %d bytes omitted]\n", len(part)-MaxFileBytes)
}

// Clean normalises the model's output into a commit message: surrounding code
// fences removed, whitespace trimmed. Nothing else is stripped.
func Clean(out string) string {
	s := strings.TrimSpace(out)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	lines = lines[1:] // drop opening fence (with or without a language)
	if strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
