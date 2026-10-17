package dshplugin

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestVersionMatchesManifest(t *testing.T) {
	version, err := Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "0.2.0" {
		t.Fatalf("Version = %q, want 0.2.0", version)
	}
	if strings.Contains(version, "+") || strings.Contains(version, " ") {
		t.Fatalf("Version = %q is not suitable for a tarball name", version)
	}
}

func TestRuntimeFilesExcludeTestsAndGoSources(t *testing.T) {
	names, err := RuntimeFileNames()
	if err != nil {
		t.Fatalf("RuntimeFileNames: %v", err)
	}
	want := []string{
		"cordis.patch.yml",
		"icon.svg",
		"index.js",
		"locale/en.json",
		"locale/zh.json",
		"package.json",
	}
	if len(names) != len(want) {
		t.Fatalf("RuntimeFileNames = %v, want %v", names, want)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if strings.Join(sorted, ",") != strings.Join(want, ",") {
		t.Fatalf("RuntimeFileNames = %v, want %v", names, want)
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "test/") || name == "embed.go" {
			t.Fatalf("runtime files contain a non-runtime entry: %s", name)
		}
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

func TestWriteTarballIsNpmCompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memctl-dsh-0.2.0.tgz")
	if err := WriteTarball(path); err != nil {
		t.Fatalf("WriteTarball: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	got := map[string]string{}
	var sawRoot bool
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if header.Name == "package/" {
			sawRoot = true
			continue
		}
		if !strings.HasPrefix(header.Name, "package/") {
			t.Fatalf("tar entry %q is outside package/", header.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[strings.TrimPrefix(header.Name, "package/")] = string(data)
	}
	if !sawRoot {
		t.Fatal("tarball has no package/ root entry")
	}
	names, err := RuntimeFileNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(names) {
		t.Fatalf("tarball entries = %v, want %v", got, names)
	}
	for _, name := range names {
		embedded, readErr := files.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got[name] != string(embedded) {
			t.Fatalf("tarball entry %s differs from the embedded source", name)
		}
	}
}

func TestEnsurePathsAreAbsoluteAndReplaceable(t *testing.T) {
	state := t.TempDir()
	tarball, err := EnsureTarball(state)
	if err != nil {
		t.Fatalf("EnsureTarball: %v", err)
	}
	if !filepath.IsAbs(tarball) || !strings.HasSuffix(tarball, "memctl-dsh-0.2.0.tgz") {
		t.Fatalf("unexpected tarball path %q", tarball)
	}
	if _, err := os.Stat(tarball); err != nil {
		t.Fatalf("tarball was not written: %v", err)
	}
	first, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureTarball(state); err != nil {
		t.Fatalf("second EnsureTarball: %v", err)
	}
	second, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("the tarball is not deterministic for the same embedded files")
	}

	dir, err := EnsureDir(state)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	version, err := Version()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(dir) || filepath.Base(dir) != version {
		t.Fatalf("unexpected plugin dir %q", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Fatalf("plugin dir is missing package.json: %v", err)
	}
}

func TestManifestHasNoInstallScriptsOrDependencies(t *testing.T) {
	data, err := files.ReadFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files      []string          `json:"files"`
		Engines    map[string]string `json:"engines"`
		Dsh        map[string]any    `json:"dsh"`
		Deps       map[string]string `json:"dependencies"`
		DevDeps    map[string]string `json:"devDependencies"`
		OptDeps    map[string]string `json:"optionalDependencies"`
		Scripts    map[string]string `json:"scripts"`
		PeerDeps   map[string]string `json:"peerDependencies"`
		Exports    map[string]string `json:"exports"`
		Private    bool              `json:"private"`
		BundlePath string            `json:"bundlePatch"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Deps)+len(manifest.DevDeps)+len(manifest.OptDeps)+len(manifest.PeerDeps) != 0 {
		t.Fatalf("plugin manifest must not declare dependencies: %+v", manifest)
	}
	for _, script := range []string{"install", "postinstall", "prepare"} {
		if manifest.Scripts[script] != "" {
			t.Fatalf("plugin manifest must not declare scripts.%s", script)
		}
	}
	if manifest.Engines["dsh"] == "" || manifest.Engines["node"] == "" {
		t.Fatalf("plugin manifest must declare engines.dsh and engines.node: %+v", manifest.Engines)
	}
	bundle, ok := manifest.Dsh["bundle"].(map[string]any)
	if !ok || bundle["patch"] != "./cordis.patch.yml" {
		t.Fatalf("plugin manifest does not declare dsh.bundle.patch: %+v", manifest.Dsh)
	}
	wantFiles := map[string]bool{
		"index.js": true, "cordis.patch.yml": true, "locale/*.json": true, "icon.svg": true,
	}
	if len(manifest.Files) != len(wantFiles) {
		t.Fatalf("files = %v, want %v", manifest.Files, wantFiles)
	}
	for _, file := range manifest.Files {
		if !wantFiles[file] {
			t.Fatalf("unexpected files entry %q", file)
		}
	}
	if manifest.Exports["./icon"] != "./icon.svg" || manifest.Exports["./locale/*.json"] != "./locale/*.json" {
		t.Fatalf("exports do not expose the display resources: %+v", manifest.Exports)
	}
}
