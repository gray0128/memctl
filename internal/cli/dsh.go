package cli

import (
	"fmt"
	"io"

	dshplugin "github.com/gray0128/memctl/plugins/dsh"
)

// runDsh implements `dsh plugin-path` and `dsh plugin-dir`. Both commands write
// only the generated plugin artifact under the runtime state root and print its
// absolute path; they never touch a DSH profile. See
// docs/04-ui-and-cli-specification.md section 1.
func runDsh(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, "dsh", "specify plugin-path or plugin-dir")
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "dsh", err)
	}

	var path string
	switch args[0] {
	case "plugin-path":
		path, err = dshplugin.EnsureTarball(cfg.StateDir)
	case "plugin-dir":
		path, err = dshplugin.EnsureDir(cfg.StateDir)
	default:
		return usageError(stderr, "dsh", fmt.Sprintf("unknown argument %q", args[0]))
	}
	if err != nil {
		return fail(stderr, "dsh", err)
	}
	fmt.Fprintln(stdout, path)
	return ExitOK
}
