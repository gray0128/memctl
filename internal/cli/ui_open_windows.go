//go:build windows

package cli

import "os/exec"

// openBrowser opens the local URL with the Windows shell URL handler. The UI
// is optional, so a failure is reported to the caller instead of being fatal.
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
