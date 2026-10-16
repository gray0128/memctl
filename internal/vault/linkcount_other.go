//go:build !unix

package vault

import "io/fs"

// hardLinkCount is unavailable on platforms without a portable hard-link count.
func hardLinkCount(fs.FileInfo) uint64 { return 0 }
