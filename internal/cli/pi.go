package cli

import (
	"fmt"
	"io"

	piplugin "github.com/gray0128/memctl/plugins/pi"
)

// runPi implements `pi plugin-dir`. The command writes only the generated
// local package directory under the runtime state root and prints its absolute
// path; it never runs `pi install` or modifies pi settings. See
// docs/08-pi-plugin.md section 8.2.
func runPi(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, "pi", "specify plugin-dir")
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "pi", err)
	}

	switch args[0] {
	case "plugin-dir":
		path, err := piplugin.EnsureDir(cfg.StateDir)
		if err != nil {
			return fail(stderr, "pi", err)
		}
		fmt.Fprintln(stdout, path)
	default:
		return usageError(stderr, "pi", fmt.Sprintf("unknown argument %q", args[0]))
	}
	return ExitOK
}
