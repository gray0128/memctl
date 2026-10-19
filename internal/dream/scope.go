package dream

import (
	"strings"
)

// protectedBaseNames are file names that hold Git or credential configuration
// inside a repository. They are neither exported nor accepted as a proposed
// change. See docs/03-workflows-and-dataflows.md section 6 step 1 and 4.
var protectedBaseNames = map[string]bool{
	".gitconfig":       true,
	".gitmodules":      true,
	".gitattributes":   true,
	".git-credentials": true,
	".netrc":           true,
	"credentials":      true,
	"id_rsa":           true,
	"id_ed25519":       true,
	"id_ecdsa":         true,
	"id_dsa":           true,
}

// protectedDirNames are directory names that never belong to exported memory
// or to a proposal: Git internals, host hooks and host credential stores. They
// are matched at any depth so a nested Git or host configuration file is
// rejected too.
var protectedDirNames = map[string]bool{
	".git": true,
	// `.github` holds the Dreaming workflow, which the local side must never
	// rewrite through a proposal: the Actions job runs with the repository's own
	// credentials. Only `dream --init-workflow` writes it.
	".github": true,
	".ssh":    true,
	".gnupg":  true,
	".aws":    true,
	".claude": true,
	".codex":  true,
	".config": true,
}

// protectedPath reports whether a root-relative path is Git or host
// configuration that must not be exported to the worker or proposed as a change.
// Comparisons are case-folded because a case-insensitive filesystem resolves
// `.GIT` and `.git` to the same directory.
func protectedPath(rel string) bool {
	lower := strings.ToLower(rel)
	segments := strings.Split(lower, "/")
	for _, segment := range segments {
		if protectedDirNames[segment] {
			return true
		}
	}
	base := segments[len(segments)-1]
	if protectedBaseNames[base] {
		return true
	}
	return strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key")
}
