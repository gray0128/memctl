package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
)

// runDream implements `dream --runner claude|codex`, `dream --show ID` and the
// stage D-2 `dream --apply ID --patch-hash HASH`. Generation and viewing are
// mutually exclusive with applying. Applying is explicitly refused until the
// explicit-application workflow of D-2 exists, so the command never returns a
// placeholder success.
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
		return fail(stderr, "dream", memerr.New(memerr.Unsupported,
			"applying a proposal is planned for stage D-2 and is not implemented yet"))
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
