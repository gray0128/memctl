//go:build !darwin && !linux && !windows

package cli

import "errors"

// openBrowser has no known handler on this platform. The server still starts;
// the user opens the printed URL manually.
func openBrowser(string) error {
	return errors.New("no browser opener is implemented on this platform")
}
