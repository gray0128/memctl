package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Env is the filesystem and process surface the installer uses. Tests replace
// it so detection never runs a real host.
type Env struct {
	// Home expands the $HOME prefix of every registered path template.
	Home string
	// StateDir is the runtime state root that holds install journals and
	// configuration backups, normally AGENT_MEMORY_STATE.
	StateDir string
	// LookPath resolves a host executable name.
	LookPath func(string) (string, error)
	// Run executes a host version command and returns its combined output.
	Run func(ctx context.Context, path string, args ...string) ([]byte, error)
	// ReadFile reads a configuration or manifest file.
	ReadFile func(string) ([]byte, error)
	// Stat reports file metadata for the original mode. A missing file returns
	// a fs.ErrNotExist error.
	Stat func(string) (os.FileInfo, error)
}

// DefaultEnv builds the process environment used by the CLI.
func DefaultEnv(stateDir string) Env {
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return Env{
		Home:     home,
		StateDir: stateDir,
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, path, args...).CombinedOutput()
		},
		ReadFile: os.ReadFile,
		Stat:     os.Stat,
	}
}

// hostVersionPattern extracts the first semver-like token from a version line.
var hostVersionPattern = regexp.MustCompile(`[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][0-9A-Za-z.\-]+)?`)

// hostVersionTimeout bounds one host version probe. It must stay well above a
// cold `--version` on a loaded machine: probing several hosts while the test
// suite compiles in parallel can otherwise turn a healthy host into an
// unparsable version and make an install report UNSUPPORTED.
const hostVersionTimeout = 10 * time.Second

type detection struct {
	hostFound      bool
	hostCommand    string
	hostVersion    string
	hostUnparsed   bool
	pluginRequired bool
	pluginFound    bool
	pluginVersion  string
	nativeFound    bool
	nativeVersion  string
	// legacyFound reports tool-managed entries in the legacy hook file. It is
	// only meaningful when the native plugin declares a LegacyConfig probe.
	legacyFound bool
	// settingsErr and legacyErr report unreadable or malformed host files that
	// prevent a reliable state decision.
	settingsErr bool
	legacyErr   bool
}

// expand resolves the $HOME and $STATE prefixes of a registered path template.
func (env Env) expand(template string) string {
	out := strings.ReplaceAll(template, "$HOME", env.Home)
	return strings.ReplaceAll(out, "$STATE", env.StateDir)
}

func (env Env) readFile(path string) ([]byte, error) {
	if env.ReadFile == nil {
		return nil, fs.ErrNotExist
	}
	return env.ReadFile(path)
}

// detect probes the host executable and any registered plugin manifest.
func (env Env) detect(spec AdapterSpec) detection {
	result := detection{pluginRequired: spec.Plugin != nil && spec.NativePlugin == nil}
	for _, command := range spec.HostCommands {
		if env.LookPath == nil {
			continue
		}
		path, err := env.LookPath(command)
		if err != nil {
			continue
		}
		result.hostFound = true
		result.hostCommand = command
		output := env.runVersion(path)
		if match := hostVersionPattern.FindString(output); match != "" {
			result.hostVersion = match
			break
		}
		result.hostUnparsed = true
		break
	}
	if spec.Plugin != nil {
		if found, version := env.probePlugin(*spec.Plugin); found {
			result.pluginFound = true
			result.pluginVersion = version
		}
	}
	if spec.NativePlugin != nil {
		found, version, settingsErr := env.probeNativePlugin(*spec.NativePlugin)
		result.nativeFound = found
		result.nativeVersion = version
		result.settingsErr = settingsErr
		if spec.NativePlugin.LegacyConfig != nil {
			legacy, err := env.probeLegacyConfig(*spec.NativePlugin.LegacyConfig)
			result.legacyFound = legacy
			result.legacyErr = err != nil
		}
	}
	return result
}

// probeNativePlugin searches the registry manifests, the default local package
// directory and the host settings for an installed native plugin.
func (env Env) probeNativePlugin(spec PluginSpec) (found bool, version string, settingsErr bool) {
	if found, version := env.probePlugin(spec); found {
		return true, version, false
	}
	if spec.DirTemplate != "" {
		if found, version := env.probeManifest(env.expand(spec.DirTemplate)); found {
			return true, version, false
		}
	}
	if spec.SettingsFile != "" {
		found, version, err := env.probeSettings(spec)
		if err != nil {
			return false, "", true
		}
		if found {
			return true, version, false
		}
	}
	return false, "", false
}

// probeSettings reads the global settings file and resolves local package
// declarations. npm, git and URL sources are covered by the manifest probes.
func (env Env) probeSettings(spec PluginSpec) (bool, string, error) {
	settingsPath := env.expand(spec.SettingsFile)
	data, err := env.readFile(settingsPath)
	if err != nil {
		if isNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, "", err
	}
	base := filepath.Dir(settingsPath)
	for _, raw := range settings.Packages {
		source := ""
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			source = text
		} else {
			var object struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(raw, &object); err != nil {
				continue
			}
			source = object.Source
		}
		if !isLocalPackageSource(source) {
			continue
		}
		path := source
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		if found, version := env.probeNamedManifest(filepath.Join(path, "package.json"), spec.Name); found {
			return true, version, nil
		}
	}
	return false, "", nil
}

// probeNamedManifest reads a package manifest and requires the expected name,
// so an unrelated local package cannot satisfy the native settings probe.
func (env Env) probeNamedManifest(path, name string) (bool, string) {
	data, err := env.readFile(path)
	if err != nil {
		return false, ""
	}
	var parsed struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Version == "" || parsed.Name != name {
		return false, ""
	}
	return true, parsed.Version
}

func isLocalPackageSource(source string) bool {
	if source == "" {
		return false
	}
	for _, prefix := range []string{"npm:", "git:", "http://", "https://", "git@"} {
		if strings.HasPrefix(source, prefix) {
			return false
		}
	}
	return true
}

// probeLegacyConfig reports whether the legacy hook file still contains entries
// owned by this tool. A valid but empty `hooks:` block is not an error.
func (env Env) probeLegacyConfig(spec LegacyConfigSpec) (bool, error) {
	data, err := env.readFile(env.expand(spec.PathTemplate))
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	lines := splitLinesKeep(string(data))
	hooksIdx := -1
	for index := range lines {
		if yamlHooksKeyPattern.MatchString(lineBody(lines[index])) {
			hooksIdx = index
			break
		}
	}
	if hooksIdx < 0 {
		return false, nil
	}
	block := lines[hooksIdx+1 : yamlBlockEnd(lines, hooksIdx)]
	items, _, _, ok := splitYAMLItems(block)
	if !ok {
		for _, line := range block {
			body := strings.TrimSpace(lineBody(line))
			if body == "" || strings.HasPrefix(body, "#") {
				continue
			}
			return false, fmt.Errorf("cannot parse the hooks block as a YAML sequence")
		}
		return false, nil
	}
	for _, item := range items {
		if strings.HasPrefix(item.id, spec.EntryPrefix) {
			return true, nil
		}
	}
	return false, nil
}

// probePlugin returns the first readable manifest that carries a version.
func (env Env) probePlugin(spec PluginSpec) (bool, string) {
	for _, manifest := range env.expandManifests(spec.Manifests) {
		if found, version := env.probeManifest(manifest); found {
			return true, version
		}
	}
	return false, ""
}

// probeManifest reads one package manifest and returns its version.
func (env Env) probeManifest(path string) (bool, string) {
	data, err := env.readFile(path)
	if err != nil {
		return false, ""
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Version == "" {
		return false, ""
	}
	return true, parsed.Version
}

// expandManifests resolves $HOME and expands glob candidates. Profile-based
// hosts such as dsh store plugins under a named profile directory, so a fixed
// manifest list cannot cover every profile name.
func (env Env) expandManifests(templates []string) []string {
	out := make([]string, 0, len(templates))
	for _, template := range templates {
		expanded := env.expand(template)
		if !strings.ContainsAny(expanded, "*?[") {
			out = append(out, expanded)
			continue
		}
		matches, err := filepath.Glob(expanded)
		if err != nil {
			continue
		}
		out = append(out, matches...)
	}
	return out
}

func (env Env) runVersion(path string) string {
	if env.Run == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostVersionTimeout)
	defer cancel()
	output, _ := env.Run(ctx, path, "--version")
	return string(output)
}

// pluginMissing reports whether the adapter requires a plugin that is absent.
func (d detection) pluginMissing() bool { return d.pluginRequired && !d.pluginFound }

// notFound reports whether the host executable was absent.
func (d detection) notFound() bool { return !d.hostFound }

var errNotExist = fs.ErrNotExist

func isNotExist(err error) bool { return errors.Is(err, errNotExist) }
