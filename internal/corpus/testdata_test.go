package corpus

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// writeTemp writes one fixture file and returns its absolute path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// writeZstd compresses plain JSONL into a zstd session file.
func writeZstd(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.v4.jsonl.zstd")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	writer, err := zstd.NewWriter(file, zstd.WithEncoderConcurrency(1))
	if err != nil {
		file.Close()
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		writer.Close()
		file.Close()
		t.Fatalf("zstd write: %v", err)
	}
	if err := writer.Close(); err != nil {
		file.Close()
		t.Fatalf("zstd close: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
	return path
}

// compressZstd encodes content as one zstd frame.
func compressZstd(content []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(content); err != nil {
		writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
