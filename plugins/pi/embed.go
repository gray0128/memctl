// Package piplugin embeds the memctl-pi extension sources and writes them to the
// runtime state directory as a local pi package directory. pi installs the npm
// package or this directory; it does not unpack a tarball.
//
// See docs/08-pi-plugin.md.
package piplugin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

//go:embed package.json index.js README.md
var files embed.FS

// RuntimeFileNames returns the embedded plugin files in a stable order. The
// test directory and Go sources are intentionally not part of the package.
func RuntimeFileNames() ([]string, error) {
	var names []string
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the embedded pi plugin files: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

var packageVersion = sync.OnceValues(func() (string, error) {
	data, err := files.ReadFile("package.json")
	if err != nil {
		return "", fmt.Errorf("read the embedded pi plugin manifest: %w", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse the embedded pi plugin manifest: %w", err)
	}
	if manifest.Version == "" {
		return "", fmt.Errorf("the embedded pi plugin manifest has no version")
	}
	return manifest.Version, nil
})

// Version returns the embedded plugin version. It is the same value npm packs
// and the adapter reports for memctl-pi.
func Version() (string, error) {
	return packageVersion()
}

// WriteDir writes every runtime file into dir, creating parents and replacing
// each file atomically. Existing files outside the embedded file list are kept.
func WriteDir(dir string) error {
	names, err := RuntimeFileNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		data, readErr := files.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("read the embedded pi plugin file %s: %w", name, readErr)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if writeErr := writeFileAtomic(target, data); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// DirPath returns the absolute directory used by `memctl pi plugin-dir`. The
// path is stable across upgrades: pi keeps the directory declaration and reads
// the replaced files on its next start.
func DirPath(stateDir string) (string, error) {
	if stateDir == "" {
		return "", fmt.Errorf("the pi plugin directory needs a state directory")
	}
	return filepath.Abs(filepath.Join(stateDir, "pi"))
}

// EnsureDir writes the plugin files to DirPath and returns the absolute path.
func EnsureDir(stateDir string) (string, error) {
	dir, err := DirPath(stateDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create the pi plugin directory: %w", err)
	}
	if err := WriteDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".memctl-*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set the mode of %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
