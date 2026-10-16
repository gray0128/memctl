//go:build linux

package cli

import "os/exec"

// openBrowser opens the local URL with xdg-open. The UI is optional, so a
// failure is reported to the caller instead of being fatal.
func openBrowser(url string) error {
	return exec.Command("xdg-open", url).Start()
}
