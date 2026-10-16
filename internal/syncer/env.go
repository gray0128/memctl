package syncer

import "strings"

// syncEnvAllow is the credential and network environment a background worker
// may receive. It is the single implementation of the whitelist defined in
// docs/01-architecture-and-modules.md section 3.4: Home/path/proxy, the user's
// SSH and askpass configuration, and the Git configuration channels. The
// worker must not read the login shell or host initialization files, so the
// parent passes only these entries.
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
}

// SyncEnv filters an environment down to the background worker's whitelist.
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
