package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
)

// runDream implements the eight mutually exclusive modes of
// docs/04-ui-and-cli-specification.md section 1: generate (--runner), show,
// apply, install the Actions workflow (--init-workflow), the internal Actions
// entry point (--worker), and the local current-agent path (--prepare,
// --propose, --list) of docs/12-session-corpus-dreaming.md section 2.3. None of
// them writes the main repository before an explicit apply.
func runDream(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	runner := ""
	show := ""
	apply := ""
	patchHash := ""
	initWorkflow := false
	worker := false
	workerRef := ""
	baseHead := ""
	prepare := false
	propose := false
	list := false
	project := ""
	maxBytes := 0
	listLimit := 0
	agentFlag := ""
	sidFlag := ""
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
		case arg == "--prepare":
			prepare = true
		case arg == "--propose":
			propose = true
		case arg == "--list":
			list = true
		case arg == "--stdin":
			// The propose input always comes from stdin; the flag exists so
			// the documented command line is explicit and a bare `--propose`
			// is not mistaken for a prompt.
		case arg == "--project":
			value, ok := next("--project")
			if !ok {
				return ExitUsageError
			}
			project = value
		case arg == "--max-bytes":
			value, ok := next("--max-bytes")
			if !ok {
				return ExitUsageError
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return usageError(stderr, "dream", "--max-bytes requires a positive integer")
			}
			maxBytes = parsed
		case arg == "--agent":
			value, ok := next("--agent")
			if !ok {
				return ExitUsageError
			}
			agentFlag = value
		case arg == "--sid":
			value, ok := next("--sid")
			if !ok {
				return ExitUsageError
			}
			sidFlag = value
		case arg == "--limit":
			value, ok := next("--limit")
			if !ok {
				return ExitUsageError
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return usageError(stderr, "dream", "--limit requires a positive integer")
			}
			listLimit = parsed
		case arg == "--json":
			// Every mode already prints JSON; the flag documents the intent.
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
	for _, value := range []bool{initWorkflow, worker, prepare, propose, list} {
		if value {
			modes++
		}
	}
	if modes != 1 {
		return usageError(stderr, "dream",
			"exactly one of --runner, --show, --apply, --init-workflow, --worker, --prepare, --propose or --list is required")
	}
	if patchHash != "" && apply == "" {
		return usageError(stderr, "dream", "--patch-hash requires --apply")
	}
	if (workerRef != "" || baseHead != "") && !worker {
		return usageError(stderr, "dream", "--ref and --base-head are only valid with --worker")
	}
	if (project != "" || maxBytes != 0 || agentFlag != "" || sidFlag != "") && !prepare && !propose {
		return usageError(stderr, "dream",
			"--project, --max-bytes, --agent and --sid are only valid with --prepare or --propose")
	}
	if project != "" && propose {
		return usageError(stderr, "dream", "--project is only valid with --prepare")
	}
	if listLimit != 0 && !list {
		return usageError(stderr, "dream", "--limit is only valid with --list")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "dream", err)
	}
	service := app.New(cfg)
	service.SetVersion(version)
	// Every commit a Dreaming mode creates (the workflow install and an applied
	// proposal) records the same provenance as a managed write, so the audit
	// view never shows it as unattributed.
	service.SetCommitSource(app.CommitSource(os.Getenv))
	if cfg.BackgroundSync {
		service.EnableBackgroundSync()
	}
	ctx := context.Background()

	switch {
	case worker:
		// The worker runs inside GitHub Actions against the checked-out job
		// workspace, not the user's memory repository. Without pinning the
		// repository, the service resolves the default ~/memory, fails to find
		// it and reports a misleading Git error.
		if workerRef == "" {
			workerRef = os.Getenv("GITHUB_REF_NAME")
		}
		workspace, workspaceErr := workerRepository(os.Getenv, cfg)
		if workspaceErr != nil {
			return fail(stderr, "dream", workspaceErr)
		}
		service = app.New(workspace)
		service.SetVersion(version)
		result, err := service.DreamWorker(ctx, workerRequestRef(workerRef), baseHead)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, result); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case prepare:
		bundle, err := service.DreamPrepareLocal(ctx, dream.PrepareRequest{
			Project:  project,
			Device:   hookio.ResolveDevice(os.Getenv),
			Agent:    hookio.ResolveAgent(agentFlag, os.Getenv),
			SID:      hookio.ResolveSID(sidFlag, os.Getenv, hookio.Input{}),
			MaxBytes: maxBytes,
		})
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, bundle); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case propose:
		var input dream.DreamProposeInput
		decoder := json.NewDecoder(io.LimitReader(stdin, proposeInputBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return fail(stderr, "dream", fmt.Errorf("INVALID_ARGUMENT: cannot decode the proposal from stdin: %w", err))
		}
		result, err := service.DreamProposeLocal(ctx, input)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if err := encodeJSON(stdout, result); err != nil {
			return fail(stderr, "dream", err)
		}
		return ExitOK
	case list:
		proposals, err := service.DreamList(ctx, listLimit)
		if err != nil {
			return fail(stderr, "dream", err)
		}
		if proposals == nil {
			proposals = []dream.Proposal{}
		}
		if err := encodeJSON(stdout, map[string]any{"proposals": proposals}); err != nil {
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

// proposeInputBytes bounds the structured proposal read from stdin. The
// documented request limit is 8 MiB; the proposal carries whole memory files,
// so it uses the same bound as every other JSON request.
const proposeInputBytes = 8 << 20

// workerRepository resolves the repository the Actions worker must operate on.
// The worker is pointed at the checked-out workspace via GITHUB_WORKSPACE, and
// an explicit AGENT_MEMORY_DIR still wins. Falling back to the configured
// default would make the worker read the runner's empty home directory.
func workerRepository(getenv func(string) string, cfg config.Config) (config.Config, error) {
	if strings.TrimSpace(getenv("AGENT_MEMORY_DIR")) != "" {
		return cfg, nil
	}
	workspace := strings.TrimSpace(getenv("GITHUB_WORKSPACE"))
	if workspace == "" {
		return config.Config{}, memerr.New(memerr.InvalidArgument,
			"the worker needs a checked-out repository; set GITHUB_WORKSPACE or AGENT_MEMORY_DIR")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return config.Config{}, memerr.New(memerr.NoRepo,
			"the worker workspace %s is not a directory", workspace)
	}
	cfg.RepoPath = config.Canonical(workspace)
	return cfg, nil
}

// workerRequestRef normalizes the branch name the workflow was triggered by.
func workerRequestRef(value string) string {
	if value == "" || len(value) >= len("refs/") && value[:5] == "refs/" {
		return value
	}
	return "refs/heads/" + value
}
