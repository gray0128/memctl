package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteAtomicSetsModeDespiteUmask covers the file-mode preservation
// invariant of docs/01-architecture-and-modules.md section 2.3: the created
// mode must not depend on the process umask or on a leftover temporary file.
func TestWriteAtomicSetsModeDespiteUmask(t *testing.T) {
	dir := t.TempDir()
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()

	previous := setUmask(0o077)
	defer setUmask(previous)

	if err := root.WriteAtomic("run.sh", []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "run.sh"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}

	// A leftover temporary file must not keep its old mode either.
	if err := os.WriteFile(filepath.Join(dir, ".run.sh"+memctlTempSuffix), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale temp: %v", err)
	}
	if err := root.WriteAtomic("run.sh", []byte("#!/bin/sh\nfalse\n"), 0o644); err != nil {
		t.Fatalf("second WriteAtomic: %v", err)
	}
	if info, err = os.Stat(filepath.Join(dir, "run.sh")); err != nil {
		t.Fatalf("stat after rewrite: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode after rewrite = %v, want 0644", info.Mode().Perm())
	}
}

// TestListManagedSkipsAtomicTempFiles covers the crash-recovery boundary: a
// hidden `.memctl-tmp` file is not managed memory and must not appear in the
// repository revision or in diagnostic scans.
func TestListManagedSkipsAtomicTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".MEMORY.md"+memctlTempSuffix), []byte("half written"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()

	paths, err := root.ListManaged()
	if err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	for _, path := range paths {
		if path == ".MEMORY.md"+memctlTempSuffix {
			t.Fatalf("ListManaged included a temporary file: %v", paths)
		}
	}
	if len(paths) != 1 || paths[0] != "MEMORY.md" {
		t.Fatalf("ListManaged = %v, want [MEMORY.md]", paths)
	}
}

// TestRevisionWithLengthDistinguishesTruncatedAssets covers P3-1: when only the
// bounded prefix of a large asset is hashed, the exact length must still change
// the digest.
func TestRevisionWithLengthDistinguishesTruncatedAssets(t *testing.T) {
	prefix := []byte("shared prefix")
	short := revisionWithLength("100644", 9000, prefix)
	long := revisionWithLength("100644", 9001, prefix)
	if short == long {
		t.Fatalf("revisions collide: %q", short)
	}
	if short == revisionWithLength("100755", 9000, prefix) {
		t.Fatalf("mode is not part of the digest: %q", short)
	}
}
