package piplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionMatchesManifest(t *testing.T) {
	version, err := Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "0.5.0" {
		t.Fatalf("Version = %q, want 0.5.0", version)
	}
}

func TestRuntimeFilesExcludeTestsAndGoSources(t *testing.T) {
	names, err := RuntimeFileNames()
	if err != nil {
		t.Fatalf("RuntimeFileNames: %v", err)
	}
	want := []string{"README.md", "index.js", "package.json"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("RuntimeFileNames = %v, want %v", names, want)
	}
}

func TestWriteDirWritesEveryRuntimeFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDir(dir); err != nil {
		t.Fatalf("WriteDir: %v", err)
	}
	names, err := RuntimeFileNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		embedded, readErr := files.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		written, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if string(written) != string(embedded) {
			t.Fatalf("%s content differs from the embedded source", name)
		}
	}
}

func TestEnsureDirIsStableAndReplaceable(t *testing.T) {
	state := t.TempDir()
	dir, err := EnsureDir(state)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if !filepath.IsAbs(dir) || filepath.Base(dir) != "pi" {
		t.Fatalf("unexpected plugin dir %q", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Fatalf("plugin dir is missing package.json: %v", err)
	}
	// A second run overwrites the same stable path without a version suffix.
	second, err := EnsureDir(state)
	if err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
	if second != dir {
		t.Fatalf("EnsureDir changed path from %q to %q", dir, second)
	}
}

func TestManifestHasNoInstallScriptsOrRuntimeDependencies(t *testing.T) {
	data, err := files.ReadFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name    string            `json:"name"`
		Files   []string          `json:"files"`
		Engines map[string]string `json:"engines"`
		Pi      struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
		Deps     map[string]string `json:"dependencies"`
		DevDeps  map[string]string `json:"devDependencies"`
		OptDeps  map[string]string `json:"optionalDependencies"`
		Scripts  map[string]string `json:"scripts"`
		PeerDeps map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "memctl-pi" {
		t.Fatalf("name = %q, want memctl-pi", manifest.Name)
	}
	if len(manifest.Deps)+len(manifest.DevDeps)+len(manifest.OptDeps) != 0 {
		t.Fatalf("plugin manifest must not declare runtime dependencies: %+v", manifest)
	}
	if len(manifest.PeerDeps) != 1 || manifest.PeerDeps["@earendil-works/pi-coding-agent"] != "*" {
		t.Fatalf("peerDependencies = %+v, want only the pi host at *", manifest.PeerDeps)
	}
	for _, script := range []string{"install", "postinstall", "prepare"} {
		if manifest.Scripts[script] != "" {
			t.Fatalf("plugin manifest must not declare scripts.%s", script)
		}
	}
	if manifest.Engines["node"] == "" {
		t.Fatalf("plugin manifest must declare engines.node: %+v", manifest.Engines)
	}
	if len(manifest.Pi.Extensions) != 1 || manifest.Pi.Extensions[0] != "./index.js" {
		t.Fatalf("pi.extensions = %v, want ./index.js", manifest.Pi.Extensions)
	}
	if len(manifest.Files) != 2 || manifest.Files[0] != "index.js" || manifest.Files[1] != "README.md" {
		t.Fatalf("files = %v, want [index.js README.md]", manifest.Files)
	}
}
