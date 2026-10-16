//go:build !darwin && !linux

package cli

import "os"

// hasTerminal is a conservative fallback for platforms without a termios
// ioctl binding: it reports false unless both streams are character devices.
func hasTerminal() bool {
	return isCharacterDevice(os.Stdin) && isCharacterDevice(os.Stderr)
}

func isCharacterDevice(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
