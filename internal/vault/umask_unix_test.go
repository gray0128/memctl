//go:build unix

package vault

import "syscall"

// setUmask sets the process umask and returns the previous value.
func setUmask(mask int) int { return syscall.Umask(mask) }
