//go:build darwin

package cli

import "os/exec"

// openBrowser opens the local URL in the default browser. The UI is optional,
// so a failure is reported to the caller instead of being fatal.
func openBrowser(url string) error {
	return exec.Command("open", url).Start()
}
