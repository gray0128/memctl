package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
)

// TestDiffCommitCoversRootTextAndBinary guards the read-only diff preview used
// by GET /api/v1/audit/{hash}/diff: the first commit diffs against the empty
// tree, text changes carry counts and a patch, binary files carry no patch,
// and invalid or unknown ids fail with a bounded contract error.

func newDiffRepo(t *testing.T) (context.Context, string) {
	t.Helper()
	if err := Available(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	t.Setenv("GIT_AUTHOR_NAME", "Memctl Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "memctl-test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Memctl Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "memctl-test@example.invalid")

	ctx := context.Background()
	repo := t.TempDir()
	if err := Init(ctx, repo); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return ctx, repo
}

func commitFile(t *testing.T, ctx context.Context, repo, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := Add(ctx, repo, name); err != nil {
		t.Fatalf("Add %s: %v", name, err)
	}
	hash, err := Commit(ctx, repo, "add "+name)
	if err != nil {
		t.Fatalf("Commit %s: %v", name, err)
	}
	return hash
}

func TestDiffCommitCoversRootTextAndBinary(t *testing.T) {
	ctx, repo := newDiffRepo(t)

	root := commitFile(t, ctx, repo, "MEMORY.md", "# Memory\n\n- first\n")
	rootDiff, err := DiffCommit(ctx, repo, root)
	if err != nil {
		t.Fatalf("DiffCommit root: %v", err)
	}
	if rootDiff.Hash != root {
		t.Fatalf("resolved hash = %q, want %q", rootDiff.Hash, root)
	}
	if len(rootDiff.Files) != 1 || rootDiff.Files[0].Path != "MEMORY.md" {
		t.Fatalf("root diff files = %+v", rootDiff.Files)
	}
	if rootDiff.Files[0].Additions != 3 || !strings.Contains(rootDiff.Files[0].Patch, "- first") {
		t.Fatalf("root diff content = %+v", rootDiff.Files[0])
	}

	second := commitFile(t, ctx, repo, "MEMORY.md", "# Memory\n\n- first\n- second\n")
	textDiff, err := DiffCommit(ctx, repo, second)
	if err != nil {
		t.Fatalf("DiffCommit text: %v", err)
	}
	if len(textDiff.Files) != 1 || textDiff.Files[0].Additions != 1 || textDiff.Files[0].Deletions != 0 {
		t.Fatalf("text diff = %+v", textDiff.Files)
	}

	if err := os.WriteFile(filepath.Join(repo, "asset.bin"), []byte{0x00, 0x01, 0xff}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := Add(ctx, repo, "asset.bin"); err != nil {
		t.Fatalf("Add binary: %v", err)
	}
	binaryCommit, err := Commit(ctx, repo, "add binary")
	if err != nil {
		t.Fatalf("Commit binary: %v", err)
	}
	binaryDiff, err := DiffCommit(ctx, repo, binaryCommit)
	if err != nil {
		t.Fatalf("DiffCommit binary: %v", err)
	}
	if len(binaryDiff.Files) != 1 || !binaryDiff.Files[0].Binary || binaryDiff.Files[0].Patch != "" {
		t.Fatalf("binary diff = %+v", binaryDiff.Files)
	}

	if _, err := DiffCommit(ctx, repo, "--help"); err == nil {
		t.Fatal("an option-like commit must be rejected")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.InvalidArgument {
		t.Fatalf("option-like commit error = %v", err)
	}
	if _, err := DiffCommit(ctx, repo, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("an unknown commit must fail")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.NotFound {
		t.Fatalf("unknown commit error = %v", err)
	}
}
