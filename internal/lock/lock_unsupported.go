//go:build !unix && !windows

package lock

import (
	"os"

	"github.com/gray0128/memctl/internal/memerr"
)

func tryFlock(file *os.File) (bool, error) {
	return false, memerr.New(memerr.Unsupported, "repository locking is not implemented on this platform")
}
