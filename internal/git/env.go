package git

import "strings"

// syncEnvAllow is the credential and network environment a Git child may
// receive. It is the single implementation of the whitelist defined in
// docs/01-architecture-and-modules.md section 3.4, used by every foreground
// Git command and by the background worker snapshot: home/path/proxy, the
// user's SSH and askpass configuration, the Git configuration channels and the
// identity overrides that a replay needs to create commits when the identity
// comes from the environment instead of repository or global configuration.
//
// Everything else the caller happens to export — host API keys, GIT_EXEC_PATH,
// GIT_EXTERNAL_DIFF and similar process-redirection variables — is not passed
// to Git or to the hooks and credential helpers it starts.
var syncEnvAllow = map[string]bool{
	"HOME":   true,
	"PATH":   true,
	"TMPDIR": true,

	"HTTP_PROXY":  true,
	"HTTPS_PROXY": true,
	"NO_PROXY":    true,
	"ALL_PROXY":   true,
	"http_proxy":  true,
	"https_proxy": true,
	"no_proxy":    true,
	"all_proxy":   true,

	"SSH_AUTH_SOCK":       true,
	"SSH_AGENT_PID":       true,
	"GIT_SSH":             true,
	"GIT_SSH_COMMAND":     true,
	"GIT_SSH_VARIANT":     true,
	"GIT_ASKPASS":         true,
	"SSH_ASKPASS":         true,
	"SSH_ASKPASS_REQUIRE": true,
	"GIT_TERMINAL_PROMPT": true,

	"GIT_CONFIG_GLOBAL":   true,
	"GIT_CONFIG_SYSTEM":   true,
	"GIT_CONFIG_NOSYSTEM": true,
	"XDG_CONFIG_HOME":     true,
	"GIT_CONFIG_COUNT":    true,

	"GIT_AUTHOR_NAME":     true,
	"GIT_AUTHOR_EMAIL":    true,
	"GIT_COMMITTER_NAME":  true,
	"GIT_COMMITTER_EMAIL": true,
}

// SyncEnv filters an environment down to the whitelist above. The repository
// redirection variables (GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, ...) are not
// on the list, so they can never reach a child either.
func SyncEnv(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if syncEnvAllow[name] || strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			kept = append(kept, entry)
		}
	}
	return kept
}
