package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gray0128/memctl/internal/update"
)

// runUpdate implements `update [--check]`. It never touches the memory
// repository; --check only reports the latest release.
func runUpdate(args []string, stdout, stderr io.Writer, version string) int {
	checkOnly := false
	for _, arg := range args {
		switch {
		case arg == "--check":
			checkOnly = true
		case strings.HasPrefix(arg, "-"):
			return usageError(stderr, "update", fmt.Sprintf("unknown flag %q", arg))
		default:
			return usageError(stderr, "update", fmt.Sprintf("unexpected argument %q", arg))
		}
	}

	opts := update.Options{CurrentVersion: version}
	var result update.Result
	var err error
	if checkOnly {
		result, err = update.Check(context.Background(), opts)
	} else {
		result, err = update.Apply(context.Background(), opts)
	}
	if err != nil {
		return fail(stderr, "update", err)
	}

	fmt.Fprintf(stdout, "current: %s\nlatest: %s\n", result.CurrentVersion, result.LatestVersion)
	switch {
	case result.Updated:
		fmt.Fprintf(stdout, "updated: %s\n", result.BinaryPath)
		fmt.Fprintf(stdout, "restart memctl to use %s\n", result.LatestVersion)
	case result.UpdateAvailable:
		fmt.Fprintln(stdout, "update available")
	default:
		fmt.Fprintln(stdout, "up to date")
	}
	return ExitOK
}
