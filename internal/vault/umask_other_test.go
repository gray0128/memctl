//go:build !unix

package vault

// setUmask has no portable equivalent; the mode test still asserts the
// documented behavior on the platforms that use a umask.
func setUmask(mask int) int { return 0 }
