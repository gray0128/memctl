package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gray0128/memctl/internal/app"
)

// runDream implements `dream --runner claude|codex`, `dream --show ID` and
// `dream --apply ID --patch-hash HASH`. Generation and viewing are mutually
// exclusive with applying. Generating never writes the main repository;
// applying commits one already-reviewed proposal through the managed
// transaction. See docs/03-workflows-and-dataflows.md section 6.
func runDream(args []string, stdout, stderr io.Writer) int {
	runner := ""
	show := ""
	apply := ""
	patchHash := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--runner":
			if index+1 >= len(args) {
				return usageError(stderr, "dream", "--runner requires a runner name")
			}
			index++
			runner = args[index]
		case arg == "--show":
			if index+1 >= len(args) {
				return usageError(stderr, "dream", "--show requires a proposal id")
			}
			index++
			show = args[index]
		case arg == "--apply":
			if index+1 >= len(args) {
				return usageError(stderr, "dream", "--apply requires a proposal id")
			}
			index++
			apply = args[index]
		case arg == "--patch-hash":
			if index+1 >= len(args) {
				return usageError(stderr, "dream", "--patch-hash requires a hash")
			}
			index++
			patchHash = args[index]
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
	if modes != 1 {
		return usageError(stderr, "dream", "exactly one of --runner, --show or --apply is required")
	}
	if patchHash != "" && apply == "" {
		return usageError(stderr, "dream", "--patch-hash requires --apply")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "dream", err)
	}
	service := app.New(cfg)
	ctx := context.Background()

	switch {
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
