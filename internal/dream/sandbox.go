package dream

import (
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// Sandbox is one platform isolation backend. Wrap returns the executable and
// argument list that runs execPath under the profile confining writes to
// workDir. A backend that is not available must report it through Available
// instead of silently running the runner unrestricted.
type Sandbox interface {
	// Name is the stable backend identifier recorded in diagnostics.
	Name() string
	// Available reports whether the backend can actually be started on this
	// host. memctl never falls back to an unsandboxed shell.
	Available() bool
	// Wrap builds the sandboxed invocation.
	Wrap(workDir, execPath string, args []string) (string, []string, error)
}

// Unavailable is the backend used when no verified platform isolation exists.
// Every attempt to wrap a runner fails with UNSUPPORTED so an unsupported host
// never runs an unrestricted runner.
type Unavailable struct{}

// Name implements Sandbox.
func (Unavailable) Name() string { return "none" }

// Available implements Sandbox.
func (Unavailable) Available() bool { return false }

// Wrap implements Sandbox and always fails.
func (Unavailable) Wrap(_, _ string, _ []string) (string, []string, error) {
	return "", nil, memerr.New(memerr.Unsupported,
		"no verified runner isolation backend is available on this platform; Dreaming generation is disabled")
}

// quoteSeatbelt escapes one string literal of a sandbox profile.
func quoteSeatbelt(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
