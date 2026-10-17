// Package dshplugin embeds the memctl-dsh Host plugin source files and writes
// them to the runtime state directory as a tarball or a local directory. The
// generated artifacts are consumed by the DSH "Plugins → Add plugin" dialog.
//
// See docs/07-dsh-plugin.md.
package dshplugin

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

//go:embed package.json cordis.patch.yml index.js icon.svg locale/*.json
var files embed.FS

// RuntimeFileNames returns the embedded plugin files in a stable order. The
// test directory and Go sources are intentionally not part of the tarball.
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
		return nil, fmt.Errorf("read the embedded dsh plugin files: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

var packageVersion = sync.OnceValues(func() (string, error) {
	data, err := files.ReadFile("package.json")
	if err != nil {
		return "", fmt.Errorf("read the embedded dsh plugin manifest: %w", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse the embedded dsh plugin manifest: %w", err)
	}
	if manifest.Version == "" {
		return "", fmt.Errorf("the embedded dsh plugin manifest has no version")
	}
	return manifest.Version, nil
})

// Version returns the embedded plugin version. It is the same value npm packs
// and the adapter reports for memctl-dsh.
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
			return fmt.Errorf("read the embedded dsh plugin file %s: %w", name, readErr)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if writeErr := writeFileAtomic(target, data); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// WriteTarball writes the runtime files as a npm-compatible tarball with the
// package/ prefix. The archive is deterministic for the same embedded files.
func WriteTarball(path string) error {
	names, err := RuntimeFileNames()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the dsh plugin directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".memctl-*")
	if err != nil {
		return fmt.Errorf("create the dsh plugin tarball: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	zw := gzip.NewWriter(tmp)
	tw := tar.NewWriter(zw)
	modTime := time.Unix(0, 0)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "package/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
		ModTime:  modTime,
	}); err != nil {
		return closeArchive(tw, zw, tmp, fmt.Errorf("write the dsh plugin tarball header: %w", err))
	}
	for _, name := range names {
		data, readErr := files.ReadFile(name)
		if readErr != nil {
			return closeArchive(tw, zw, tmp, fmt.Errorf("read the embedded dsh plugin file %s: %w", name, readErr))
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     "package/" + name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  modTime,
		}); err != nil {
			return closeArchive(tw, zw, tmp, fmt.Errorf("write the dsh plugin tarball entry %s: %w", name, err))
		}
		if _, err := tw.Write(data); err != nil {
			return closeArchive(tw, zw, tmp, fmt.Errorf("write the dsh plugin tarball content %s: %w", name, err))
		}
	}
	if err := tw.Close(); err != nil {
		return closeArchive(nil, zw, tmp, fmt.Errorf("finish the dsh plugin tarball: %w", err))
	}
	if err := zw.Close(); err != nil {
		return closeArchive(nil, nil, tmp, fmt.Errorf("finish the dsh plugin compression: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return closeArchive(nil, nil, tmp, fmt.Errorf("flush the dsh plugin tarball: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close the dsh plugin tarball: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace the dsh plugin tarball %s: %w", path, err)
	}
	return nil
}

func closeArchive(tw *tar.Writer, zw *gzip.Writer, file *os.File, cause error) error {
	if tw != nil {
		_ = tw.Close()
	}
	if zw != nil {
		_ = zw.Close()
	}
	_ = file.Close()
	return cause
}

// DirPath returns the absolute directory used by `memctl dsh plugin-dir`.
func DirPath(stateDir string) (string, error) {
	version, err := Version()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(stateDir, "dsh", version))
}

// TarballPath returns the absolute tarball path used by
// `memctl dsh plugin-path`.
func TarballPath(stateDir string) (string, error) {
	version, err := Version()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(stateDir, "dsh", "memctl-dsh-"+version+".tgz"))
}

// EnsureDir writes the plugin files to DirPath and returns the absolute path.
func EnsureDir(stateDir string) (string, error) {
	dir, err := DirPath(stateDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create the dsh plugin directory: %w", err)
	}
	if err := WriteDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsureTarball writes the plugin tarball to TarballPath and returns the
// absolute path.
func EnsureTarball(stateDir string) (string, error) {
	path, err := TarballPath(stateDir)
	if err != nil {
		return "", err
	}
	if err := WriteTarball(path); err != nil {
		return "", err
	}
	return path, nil
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
