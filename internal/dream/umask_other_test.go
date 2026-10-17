//go:build !unix

package dream

// setUmask has no portable equivalent; the mode test still asserts the
// documented behavior on the platforms that use a umask.
func setUmask(int) int { return 0 }
