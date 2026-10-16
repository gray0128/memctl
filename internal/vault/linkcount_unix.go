//go:build unix

package vault

import (
	"io/fs"
	"syscall"
)

// hardLinkCount returns the number of hard links to a regular file, or 0 when
// the platform does not expose it.
func hardLinkCount(info fs.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 0
}
