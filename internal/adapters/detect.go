package adapters

import (
	"context"
	"encoding/json"
	"errors"
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

// hostVersionTimeout bounds one host version probe.
const hostVersionTimeout = 3 * time.Second

type detection struct {
	hostFound      bool
	hostCommand    string
	hostVersion    string
	hostUnparsed   bool
	pluginRequired bool
	pluginFound    bool
	pluginVersion  string
}

// expand resolves the $HOME prefix of a registered path template.
func (env Env) expand(template string) string {
	return strings.ReplaceAll(template, "$HOME", env.Home)
}

func (env Env) readFile(path string) ([]byte, error) {
	if env.ReadFile == nil {
		return nil, fs.ErrNotExist
	}
	return env.ReadFile(path)
}

// detect probes the host executable and any required plugin manifest.
func (env Env) detect(spec AdapterSpec) detection {
	result := detection{pluginRequired: spec.Plugin != nil}
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
	if spec.Plugin == nil {
		return result
	}
	for _, manifest := range env.expandManifests(spec.Plugin.Manifests) {
		data, err := env.readFile(manifest)
		if err != nil {
			continue
		}
		var parsed struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil || parsed.Version == "" {
			continue
		}
		result.pluginFound = true
		result.pluginVersion = parsed.Version
		break
	}
	return result
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
