package app

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoSourcesAreGofmtFormatted is the repository gofmt gate: every Go file
// under cmd/ and internal/ must equal its gofmt output, so an unformatted file
// fails `go test ./...` instead of slipping through review. It uses go/format,
// the library gofmt is built on, so the check needs no external binary.
func TestGoSourcesAreGofmtFormatted(t *testing.T) {
	root := moduleRoot(t)
	var unformatted []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			formatted, err := format.Source(content)
			if err != nil {
				return err
			}
			if !bytes.Equal(content, formatted) {
				rel, _ := filepath.Rel(root, path)
				unformatted = append(unformatted, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(unformatted) > 0 {
		t.Fatalf("files are not gofmt formatted; run gofmt -w on them:\n%s", strings.Join(unformatted, "\n"))
	}
}

// moduleRoot returns the directory holding go.mod, found upward from the
// package directory the test runs in.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
