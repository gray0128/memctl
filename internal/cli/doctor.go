package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/memerr"
)

// runDoctor implements `doctor [--json] [--fix]`. The default is a read-only
// diagnosis: it reports the documented levels and returns 1 when any error-level
// issue is found, 0 otherwise. `--fix` needs the managed write API and lands
// with stage C-1, so it is rejected explicitly instead of pretending success.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	fix := false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		case "--fix":
			fix = true
		default:
			return usageError(stderr, "doctor", fmt.Sprintf("unknown flag %q", arg))
		}
	}
	if fix {
		return fail(stderr, "doctor", memerr.New(memerr.Unsupported,
			"--fix applies deterministic repairs through the managed write API and is implemented in stage C-1; this stage provides the read-only diagnosis"))
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "doctor", err)
	}
	report, err := app.New(cfg).Doctor(context.Background())
	if err != nil {
		return fail(stderr, "doctor", err)
	}

	if asJSON {
		if err := encodeJSON(stdout, report); err != nil {
			return fail(stderr, "doctor", err)
		}
	} else {
		printDoctor(stdout, report)
	}
	if report.Counts.Error > 0 {
		return ExitError
	}
	return ExitOK
}

func printDoctor(stdout io.Writer, report doctor.Report) {
	fmt.Fprintf(stdout, "revision: %s\ncheckedAt: %s\nerror: %d warning: %d info: %d total: %d\n",
		report.Revision, report.CheckedAt, report.Counts.Error, report.Counts.Warning, report.Counts.Info, report.Counts.Total)
	if len(report.Issues) == 0 {
		fmt.Fprintln(stdout, "no issues found")
		return
	}
	for _, issue := range report.Issues {
		location := ""
		if issue.FilePath != "" {
			location = " " + issue.FilePath
			if issue.LineNumber > 0 {
				location += fmt.Sprintf(":%d", issue.LineNumber)
			}
		}
		fmt.Fprintf(stdout, "%s %s%s: %s\n", issue.Severity, issue.Code, location, issue.Message)
		if issue.Suggestion != "" {
			fmt.Fprintf(stdout, "  suggestion: %s\n", issue.Suggestion)
		}
	}
}
