package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/dream"
)

// runDream implements the five mutually exclusive modes of
// docs/04-ui-and-cli-specification.md section 1: generate (--runner), show,
// apply, install the Actions workflow (--init-workflow) and the internal Actions
// entry point (--worker). Generating never writes the main repository; applying
// commits one already-reviewed proposal through the managed transaction. See
// docs/11-github-actions-dreaming.md sections 2.1 and 2.5.
func runDream(args []string, stdout, stderr io.Writer, version string) int {
	runner := ""
	show := ""
	apply := ""
	patchHash := ""
	initWorkflow := false
	worker := false
	workerRef := ""
	baseHead := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		next := func(flag string) (string, bool) {
			if index+1 >= len(args) {
				usageError(stderr, "dream", flag+" requires a value")
				return "", false
			}
			index++
			return args[index], true
		}
		switch {
		case arg == "--runner":
			value, ok := next("--runner")
			if !ok {
				return ExitUsageError
			}
			runner = value
		case arg == "--show":
			value, ok := next("--show")
			if !ok {
				return ExitUsageError
			}
			show = value
		case arg == "--apply":
			value, ok := next("--apply")
			if !ok {
				return ExitUsageError
			}
			apply = value
		case arg == "--patch-hash":
			value, ok := next("--patch-hash")
			if !ok {
				return ExitUsageError
			}
			patchHash = value
		case arg == "--init-workflow":
			initWorkflow = true
		case arg == "--worker":
			worker = true
		case arg == "--ref":
			value, ok := next("--ref")
			if !ok {
				return ExitUsageError
			}
			workerRef = value
		case arg == "--base-head":
			value, ok := next("--base-head")
			if !ok {
				return ExitUsageError
			}
			baseHead = value
		default:
			return usageError(stderr, "dream", fmt.Sprintf("unknown argument %q", arg))
		}
	}

	modes := 0
	for _, value := range []string{runner, show, apply} {
		if value != "" {
			modes++
		}
	}
	if initWorkflow {
		modes++
	}
	if worker {
		modes++
	}
	if modes != 1 {
		return usageError(stderr, "dream", "exactly one of --runner, --show, --apply, --init-workflow or --worker is required")
	}
	if patchHash != "" && apply == "" {
		return usageError(stderr, "dream", "--patch-hash requires --apply")
	}
	if (workerRef != "" || baseHead != "") && !worker {
		return usageError(stderr, "dream", "--ref and --base-head are only valid with --worker")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "dream", err)
	}
	service := app.New(cfg)
	ctx := context.Background()

	switch {
	case worker:
		// The worker runs inside GitHub Actions, where the workflow passes the
		// request branch and the baseline commit.
		if workerRef == "" {
			workerRef = os.Getenv("GITHUB_REF_NAME")
		}
		result, err := service.DreamWorker(ctx, workerRequestRef(workerRef), baseHead)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, result); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case initWorkflow:
		result, err := service.InitDreamWorkflow(ctx, version)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, result); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case apply != "":
		if patchHash == "" {
			return usageError(stderr, "dream", "--apply requires --patch-hash HASH")
		}
		result, err := service.ApplyDream(ctx, apply, patchHash)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, result); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case show != "":
		proposal, err := service.DreamProposal(ctx, show)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, proposal); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	default:
		if !dream.KnownProvider(runner) {
			return usageError(stderr, "dream", fmt.Sprintf(
				"unknown model provider %q; supported providers are %s", runner, dream.ProviderIDsList()))
		}
		proposal, err := service.DreamPreview(ctx, runner, "")
		if err != nil {
			// A persisted failed proposal is still reported on stdout so the
			// operator can inspect its error and warnings; a request rejected
			// before persistence has nothing to report.
			if proposal.ProposalID != "" {
				if encodeErr := encodeJSON(stdout, proposal); encodeErr != nil {
					return fail(stderr, "dream", encodeErr)
				}
			}
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, proposal); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	}
}

// workerRequestRef normalizes the branch name the workflow was triggered by.
func workerRequestRef(value string) string {
	if value == "" || len(value) >= len("refs/") && value[:5] == "refs/" {
		return value
	}
	return "refs/heads/" + value
}
