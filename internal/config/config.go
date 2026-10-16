// Package config resolves the memory repository and runtime state locations
// from the documented environment variables. See
// docs/04-ui-and-cli-specification.md section 2.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

const (
	EnvRepoDir  = "AGENT_MEMORY_DIR"
	EnvStateDir = "AGENT_MEMORY_STATE"
	// EnvBackground disables best-effort background synchronization when set
	// to 0/false/off. It is on by default.
	EnvBackground = "AGENT_MEMORY_BACKGROUND"

	DefaultRepoDirName  = "memory"
	DefaultStateDirName = ".agent-memory"
)

// Config binds one process to one memory repository and one state root.
type Config struct {
	// RepoPath is the canonical absolute path of the memory repository.
	RepoPath string
	// StateDir is the canonical absolute path of the runtime state root.
	StateDir string
	// BackgroundSync enables best-effort background synchronization after a
	// managed write and for the automatic Hook path.
	BackgroundSync bool
}

// Load resolves the configuration from the given environment accessor
// (normally os.Getenv). It does not touch the filesystem.
func Load(getenv func(string) string) (Config, error) {
	home := getenv("HOME")
	repoPath := getenv(EnvRepoDir)
	stateDir := getenv(EnvStateDir)

	if repoPath == "" {
		if home == "" {
			return Config{}, memerr.New(memerr.InvalidArgument, "HOME is not set; cannot resolve the default memory repository path")
		}
		repoPath = filepath.Join(home, DefaultRepoDirName)
	}
	if stateDir == "" {
		if home == "" {
			return Config{}, memerr.New(memerr.InvalidArgument, "HOME is not set; cannot resolve the default state path")
		}
		stateDir = filepath.Join(home, DefaultStateDirName)
	}

	return Config{
		RepoPath:       Canonical(repoPath),
		StateDir:       Canonical(stateDir),
		BackgroundSync: backgroundEnabled(getenv(EnvBackground)),
	}, nil
}

// backgroundEnabled reports whether background synchronization is enabled.
// Every value except an explicit false-like value keeps the default on.
func backgroundEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// Canonical returns an absolute, cleaned path with symbolic links resolved.
// When only a prefix of the path exists, the existing prefix is resolved and
// the remaining components are appended, so a not-yet-created repository path
// still matches what Git reports after creation.
func Canonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

	dir, rest := abs, ""
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// RepoID is the local state partition key: the SHA-256 of the canonical
// repository root. It is not a cross-device identity.
func RepoID(repoPath string) string {
	sum := sha256.Sum256([]byte(Canonical(repoPath)))
	return "sha256:" + hex.EncodeToString(sum[:])
}
