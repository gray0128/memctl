package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/doctor"
)

// runDoctor implements `doctor [--json] [--fix]`. The default is a read-only
// diagnosis: it reports the documented levels and returns 1 when any error-level
// issue is found, 0 otherwise. `--fix` applies the deterministic repairs through
// the managed write API; codes without a deterministic repair are refused with
// UNSUPPORTED instead of guessed.
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

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "doctor", err)
	}
	service := app.New(cfg)
	ctx := context.Background()
	if fix {
		return runDoctorFix(ctx, service, asJSON, stdout, stderr)
	}

	report, err := service.Doctor(ctx)
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

// runDoctorFix applies the deterministic repairs and reports the resulting
// diagnosis. It still returns 1 when error-level issues remain after the fix.
func runDoctorFix(ctx context.Context, service *app.App, asJSON bool, stdout, stderr io.Writer) int {
	// The repair binds the same repository revision the caller just diagnosed.
	report, err := service.Doctor(ctx)
	if err != nil {
		return fail(stderr, "doctor", err)
	}
	result, err := service.DoctorFix(ctx, app.DoctorFixRequest{BaseRevision: report.Revision})
	if asJSON {
		payload := struct {
			Report doctor.Report       `json:"report"`
			Result *app.MutationResult `json:"result,omitempty"`
		}{Report: result.Report, Result: result.Result}
		if encodeErr := encodeJSON(stdout, payload); encodeErr != nil {
			return fail(stderr, "doctor", encodeErr)
		}
	} else {
		printDoctor(stdout, result.Report)
		if result.Result != nil {
			fmt.Fprintf(stdout, "applied: transaction %s commit %s\n", result.Result.TransactionID, result.Result.Commit)
		} else {
			fmt.Fprintln(stdout, "no deterministic fixes applied")
		}
	}
	if err != nil {
		return fail(stderr, "doctor", err)
	}
	if result.Report.Counts.Error > 0 {
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
