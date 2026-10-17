//go:build darwin

package dream

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// seatbelt is the macOS Seatbelt (sandbox-exec) backend. It is the only
// isolation backend verified by this project so far. The profile denies
// everything by default and allows writes only inside the temporary work
// directory, so the main repository and the user configuration stay
// non-writable even though the runner may read them.
type seatbelt struct{}

// platformSandbox returns the backend for the current platform.
func platformSandbox() Sandbox { return seatbelt{} }

// Name implements Sandbox.
func (seatbelt) Name() string { return "seatbelt" }

// Available implements Sandbox. It reports false when sandbox-exec is missing
// so an unsupported host never runs a runner without isolation.
func (seatbelt) Available() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// Wrap implements Sandbox. The profile allows writes under workDir only;
// execution is additionally restricted to the runner's own directory and the
// trusted system roots.
func (seatbelt) Wrap(workDir, execPath string, args []string) (string, []string, error) {
	realWork, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		realWork = workDir
	}
	execDir := filepath.Dir(execPath)
	if resolved, err := filepath.EvalSymlinks(execDir); err == nil {
		execDir = resolved
	}
	profile := seatbeltProfile(realWork, execDir, execPath)
	wrapped := append([]string{"-p", profile, execPath}, args...)
	return "sandbox-exec", wrapped, nil
}

// seatbeltProfile builds the deny-by-default confinement profile. Writable
// paths are limited to the temporary work directory plus the standard devices;
// the main repository and the user configuration are therefore not writable
// even though they remain readable.
func seatbeltProfile(workDir, execDir, execPath string) string {
	var b strings.Builder
	b.WriteString("(version 1)")
	b.WriteString("(deny default)")
	b.WriteString("(allow process-fork)")
	b.WriteString("(allow process-exec")
	for _, root := range []string{execDir, execPath, "/usr", "/bin", "/sbin", "/System", "/Library", "/opt", "/usr/local"} {
		if root == "" {
			continue
		}
		b.WriteString(" (subpath ")
		b.WriteString(quoteSeatbelt(root))
		b.WriteString(")")
	}
	b.WriteString(")")
	b.WriteString("(allow signal)")
	b.WriteString("(allow file-read*)")
	b.WriteString("(allow file-write*")
	for _, writable := range []string{workDir, "/dev/null", "/dev/zero", "/dev/stdout", "/dev/stderr", "/dev/tty"} {
		b.WriteString(" (")
		if strings.HasPrefix(writable, "/dev/") {
			b.WriteString("literal ")
		} else {
			b.WriteString("subpath ")
		}
		b.WriteString(quoteSeatbelt(writable))
		b.WriteString(")")
	}
	b.WriteString(")")
	b.WriteString("(allow network*)")
	b.WriteString("(allow mach*)")
	b.WriteString("(allow sysctl-read)")
	b.WriteString("(allow ipc-posix*)")
	b.WriteString("(allow system-socket)")
	return b.String()
}
