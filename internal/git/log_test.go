package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
)

// TestLogRejectsOptionLikeBefore guards the audit pagination cursor: a
// client-supplied value must never be interpreted by Git as an option, because
// `--output=<file>` would write an arbitrary path.
func TestLogRejectsOptionLikeBefore(t *testing.T) {
	ctx, repo := newDiffRepo(t)
	commitFile(t, ctx, repo, "a.md", "x\n")

	out := filepath.Join(t.TempDir(), "leak.txt")
	_, err := Log(ctx, repo, 10, "--output="+out)
	contractErr, ok := memerr.As(err)
	if !ok || contractErr.Code != memerr.InvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for an option-like before, got %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatalf("git wrote %s from an option-like before value", out)
	}
}

// TestLogAcceptsHexCursor checks that a real commit id still works as a cursor:
// the log continues strictly older than that commit and excludes the cursor
// itself from the next page.
func TestLogAcceptsHexCursor(t *testing.T) {
	ctx, repo := newDiffRepo(t)
	first := commitFile(t, ctx, repo, "a.md", "x\n")
	second := commitFile(t, ctx, repo, "b.md", "y\n")

	commits, err := Log(ctx, repo, 10, second)
	if err != nil {
		t.Fatalf("Log with a hex cursor: %v", err)
	}
	if len(commits) != 1 || commits[0].Hash != first {
		t.Fatalf("expected only %s older than %s, got %+v", first, second, commits)
	}
}
