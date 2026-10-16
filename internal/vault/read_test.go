package vault_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

func openTestRoot(t *testing.T) (*vault.Root, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := vault.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

// TestReadFileCoversF04 checks the readable, missing, non-text and over-limit
// cases required by F04, including that the revision is stable and usable as a
// later write baseline.
func TestReadFileCoversF04(t *testing.T) {
	root, dir := openTestRoot(t)
	content := "# Memory\n\n## Index\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	file, err := root.ReadFile("MEMORY.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if file.RawContent != content {
		t.Fatalf("RawContent = %q, want %q", file.RawContent, content)
	}
	if file.RelPath != "MEMORY.md" {
		t.Fatalf("RelPath = %q", file.RelPath)
	}
	if file.Kind != vault.KindEntryPoint {
		t.Fatalf("Kind = %q, want entrypoint", file.Kind)
	}
	if !file.Editable {
		t.Fatal("text file must be editable")
	}
	if file.SizeBytes != int64(len(content)) {
		t.Fatalf("SizeBytes = %d, want %d", file.SizeBytes, len(content))
	}
	if file.FileRevision != vault.FileRevision("100644", []byte(content)) {
		t.Fatalf("FileRevision = %q does not match the content baseline", file.FileRevision)
	}

	again, err := root.ReadFile("MEMORY.md")
	if err != nil {
		t.Fatalf("second ReadFile: %v", err)
	}
	if again.FileRevision != file.FileRevision {
		t.Fatalf("revision changed between identical reads: %q != %q", again.FileRevision, file.FileRevision)
	}

	executable := filepath.Join(dir, "tool.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatalf("write tool.sh: %v", err)
	}
	script, err := root.ReadFile("tool.sh")
	if err != nil {
		t.Fatalf("ReadFile(tool.sh): %v", err)
	}
	if script.Kind != vault.KindScript {
		t.Fatalf("Kind = %q, want script", script.Kind)
	}
	if script.FileRevision != vault.FileRevision("100755", []byte("#!/bin/sh\ntrue\n")) {
		t.Fatalf("executable revision must encode mode 100755, got %q", script.FileRevision)
	}
}

func TestReadFileFailures(t *testing.T) {
	root, dir := openTestRoot(t)
	if err := os.Mkdir(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latin1.txt"), []byte{0xff, 0xfe, 'x'}, 0o644); err != nil {
		t.Fatalf("write latin1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("a", vault.MaxTextFileBytes+1)), 0o644); err != nil {
		t.Fatalf("write big: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exact-limit.txt"), []byte(strings.Repeat("b", vault.MaxTextFileBytes)), 0o644); err != nil {
		t.Fatalf("write exact limit: %v", err)
	}

	cases := []struct {
		name string
		path string
		code memerr.Code
	}{
		{"missing file", "missing.md", memerr.NotFound},
		{"directory", "notes", memerr.InvalidPath},
		{"binary with NUL", "binary.bin", memerr.Unsupported},
		{"invalid UTF-8", "latin1.txt", memerr.Unsupported},
		{"over limit", "big.txt", memerr.LimitExceeded},
		{"path escape", "../outside.md", memerr.InvalidPath},
		{"absolute path", "/etc/hosts", memerr.InvalidPath},
		{"git internals", ".git/config", memerr.InvalidPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := root.ReadFile(tc.path)
			contractErr, ok := memerr.As(err)
			if !ok {
				t.Fatalf("ReadFile(%q) = %v, want a contract error", tc.path, err)
			}
			if contractErr.Code != tc.code {
				t.Fatalf("ReadFile(%q) code = %s, want %s", tc.path, contractErr.Code, tc.code)
			}
		})
	}

	// P03: the limit is a boundary, not an off-by-one rejection.
	if _, err := root.ReadFile("exact-limit.txt"); err != nil {
		t.Fatalf("file at the exact limit must be readable: %v", err)
	}
}

// TestReadFileRejectsSymlinks covers the P02 read cases: neither a link target
// nor a link component may lead outside the repository.
func TestReadFileRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	root, dir := openTestRoot(t)

	outsideDir := t.TempDir()
	secret := "outside-secret-content"
	outsideFile := filepath.Join(outsideDir, "secret.md")
	if err := os.WriteFile(outsideFile, []byte(secret), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "link.md")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dir, "linked-dir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	for _, path := range []string{"link.md", "linked-dir/secret.md"} {
		file, err := root.ReadFile(path)
		contractErr, ok := memerr.As(err)
		if !ok || contractErr.Code != memerr.Unsupported {
			t.Fatalf("ReadFile(%q) = %v, want UNSUPPORTED", path, err)
		}
		if strings.Contains(file.RawContent, secret) {
			t.Fatalf("ReadFile(%q) leaked content from outside the repository", path)
		}
	}
}
