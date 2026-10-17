package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsageError = 2
)

const usageText = `Usage:
  memctl <subcommand> [flags] [-- command...]

Available Commands:
  version              Print memctl version information
  help                 Show this help message
  init [remote-url]    Initialize a local memory repository (--confirm-private)
  read <path>          Read a memory file or structured revision (--json)
  write <path>         Write a memory file with a baseline check (--base REV --stdin)
  sync --wait          Foreground remote synchronization
  sync                 Trigger best-effort background synchronization
  sync --capture       Capture serial external edits and commit them
  end                  Trigger background synchronization at session end
  start                SessionStart hook: synchronize and output context (--stdin --json)
  context              UserPromptSubmit hook: synchronize and output context (--once --json)
  pull                 Silent hook synchronization (--once)
  tag                  Print canonical session source URI (--agent NAME --sid ID)
  wrap                 Wrap a command with session identity and sync (--agent NAME -- cmd...)
  resolve              Inspect, apply, or abort rebase conflicts (--json | --apply | --abort)
  recover              Inspect or rollback incomplete transactions (--status | --rollback)
  doctor               Run read-only repository diagnostics (--json)
  dream                Generate, inspect or apply Dreaming proposals (--runner NAME | --show ID | --apply ID --patch-hash HASH)
  install [agent...]   Inspect or install agent adapter configurations (--all --dry-run --status)
  dsh plugin-path      Print the absolute path of the embedded memctl-dsh tarball
  dsh plugin-dir       Print the absolute path of the embedded memctl-dsh directory
  update               Update memctl from GitHub Releases (--check)
  ui                   Serve the embedded local Web UI (--port 18420 --no-open --readonly)

Run "memctl help" for usage details.
`

// Run executes the CLI command and returns an exit code per docs/04-ui-and-cli-specification.md.
// version and help never access the memory repository, state directory, Git, or Node.js.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "memctl: missing subcommand")
		fmt.Fprint(stderr, usageText)
		return ExitUsageError
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "version", "--version", "-v":
		if len(rest) > 0 {
			fmt.Fprintf(stderr, "memctl version: unexpected arguments: %s\n", strings.Join(rest, " "))
			return ExitUsageError
		}
		v := strings.TrimSpace(version)
		if v == "" {
			v = "v2.0.0-dev"
		}
		fmt.Fprintf(stdout, "memctl %s\n", v)
		return ExitOK

	case "help", "--help", "-h":
		if len(rest) > 0 {
			fmt.Fprintf(stderr, "memctl help: unexpected arguments: %s\n", strings.Join(rest, " "))
			return ExitUsageError
		}
		fmt.Fprint(stdout, usageText)
		return ExitOK

	case "init":
		return runInit(rest, stdout, stderr)

	case "read":
		return runRead(rest, stdout, stderr)

	case "write":
		return runWrite(rest, stdin, stdout, stderr)

	case "sync":
		return runSync(rest, stdin, stdout, stderr)

	case "end":
		return runEnd(rest, stdin, stdout, stderr)

	case "start":
		return runStart(rest, stdin, stdout, stderr)

	case "context":
		return runContext(rest, stdin, stdout, stderr)

	case "pull":
		return runPull(rest, stdin, stdout, stderr)

	case "tag":
		return runTag(rest, stdout, stderr)

	case "wrap":
		return runWrap(rest, stdin, stdout, stderr)

	case "resolve":
		return runResolve(rest, stdout, stderr)

	case "recover":
		return runRecover(rest, stdout, stderr)

	case "doctor":
		return runDoctor(rest, stdout, stderr)

	case "dream":
		return runDream(rest, stdout, stderr)

	case "install":
		return runInstall(rest, stdout, stderr)

	case "dsh":
		return runDsh(rest, stdout, stderr)

	case "update":
		return runUpdate(rest, stdout, stderr, version)

	case "ui":
		return runUI(rest, stdout, stderr)
	}

	if strings.HasPrefix(sub, "-") {
		fmt.Fprintf(stderr, "memctl: unknown flag %q\n", sub)
		fmt.Fprint(stderr, usageText)
		return ExitUsageError
	}

	fmt.Fprintf(stderr, "memctl: unknown command %q\n", sub)
	fmt.Fprint(stderr, usageText)
	return ExitUsageError
}

// runInit implements `init [remote-url] --confirm-private`. Without a URL it
// creates or reuses a local-only repository. With a URL it connects to an
// empty remote, clones a remote with history, or attaches a remote to an
// existing local repository.
func runInit(args []string, stdout, stderr io.Writer) int {
	remoteURL := ""
	confirmPrivate := false
	for _, arg := range args {
		switch {
		case arg == "--confirm-private":
			confirmPrivate = true
		case strings.HasPrefix(arg, "-"):
			return usageError(stderr, "init", fmt.Sprintf("unknown flag %q", arg))
		case remoteURL == "":
			remoteURL = arg
		default:
			return usageError(stderr, "init", fmt.Sprintf("unexpected argument %q", arg))
		}
	}
	if remoteURL != "" && !confirmPrivate {
		return usageError(stderr, "init", "connecting a remote requires --confirm-private to confirm the remote is private and owned by you")
	}
	if remoteURL == "" && confirmPrivate {
		return usageError(stderr, "init", "--confirm-private requires a remote URL")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "init", err)
	}
	result, err := app.New(cfg).Init(context.Background(), app.InitOptions{
		RemoteURL:      remoteURL,
		ConfirmPrivate: confirmPrivate,
		Interactive:    hasTerminal(),
	})
	printInitResult(stdout, result)
	if err != nil {
		return fail(stderr, "init", err)
	}
	return ExitOK
}

func printInitResult(stdout io.Writer, result app.InitResult) {
	if result.RepoPath == "" {
		return
	}
	fmt.Fprintf(stdout, "memory repo: %s\nstate: %s\n", result.RepoPath, result.State)
	if result.Head != "" {
		fmt.Fprintf(stdout, "head: %s\n", result.Head)
	}
	if result.RemoteState != "" {
		fmt.Fprintf(stdout, "remote: %s\n", result.RemoteState)
	}
}

// runRead implements `read <path> [--json]`.
func runRead(args []string, stdout, stderr io.Writer) int {
	relPath := ""
	asJSON := false
	for _, arg := range args {
		switch {
		case arg == "--json":
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			return usageError(stderr, "read", fmt.Sprintf("unknown flag %q", arg))
		case relPath == "":
			relPath = arg
		default:
			return usageError(stderr, "read", fmt.Sprintf("unexpected argument %q", arg))
		}
	}
	if relPath == "" {
		return usageError(stderr, "read", "missing required <path> argument")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "read", err)
	}
	file, err := app.New(cfg).ReadFile(context.Background(), relPath)
	if err != nil {
		return fail(stderr, "read", err)
	}

	if !asJSON {
		fmt.Fprint(stdout, file.RawContent)
		return ExitOK
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	payload := struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}{
		FilePath:     file.RelPath,
		FileRevision: file.FileRevision,
		RawContent:   file.RawContent,
	}
	if err := encoder.Encode(payload); err != nil {
		return fail(stderr, "read", memerr.Wrap(memerr.IOFailed, err, "cannot write JSON output"))
	}
	return ExitOK
}

// runWrite implements `write <path> --base REV --stdin [--agent NAME] [--sid ID]`.
// stdin is the file's new text, not Hook JSON.
func runWrite(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	relPath := ""
	base := ""
	baseSet := false
	useStdin := false
	agent := ""
	sid := ""

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--stdin":
			useStdin = true
		case arg == "--base" || arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "write", arg+" requires a value")
			}
			index++
			value := args[index]
			switch arg {
			case "--base":
				base, baseSet = value, true
			case "--agent":
				agent = value
			case "--sid":
				sid = value
			}
		case strings.HasPrefix(arg, "--base="):
			base, baseSet = strings.TrimPrefix(arg, "--base="), true
		case strings.HasPrefix(arg, "--agent="):
			agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			sid = strings.TrimPrefix(arg, "--sid=")
		case strings.HasPrefix(arg, "-"):
			return usageError(stderr, "write", fmt.Sprintf("unknown flag %q", arg))
		case relPath == "":
			relPath = arg
		default:
			return usageError(stderr, "write", fmt.Sprintf("unexpected argument %q", arg))
		}
	}

	if relPath == "" {
		return usageError(stderr, "write", "missing required <path> argument")
	}
	if !baseSet {
		return usageError(stderr, "write", "missing required --base revision (a fileRevision or absent)")
	}
	if !useStdin {
		return usageError(stderr, "write", "missing required --stdin flag")
	}

	content, err := readLimited(stdin, vault.MaxTextFileBytes)
	if err != nil {
		return fail(stderr, "write", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "write", err)
	}
	service := app.New(cfg)
	if cfg.BackgroundSync {
		service.EnableBackgroundSync()
	}
	result, err := service.Apply(context.Background(), app.MutationRequest{
		Changes: []app.FileChange{{
			Path:             relPath,
			ExpectedRevision: base,
			Action:           app.ActionPut,
			RawContent:       string(content),
		}},
		Agent:     hookio.ResolveAgent(agent, os.Getenv),
		SessionID: hookio.ResolveSID(sid, os.Getenv, hookio.Input{}),
		Device:    hookio.ResolveDevice(os.Getenv),
	})
	if err != nil {
		return fail(stderr, "write", err)
	}
	fmt.Fprintf(stdout, "transaction: %s\ncommit: %s\nrevision: %s\nsync: %s\n",
		result.TransactionID, result.Commit, result.Revision, result.SyncState)
	return ExitOK
}

// runSync implements `sync --wait` for foreground remote synchronization and
// `sync --capture` for explicit serial external edits. The automatic Hook path
// and background derivation belong to A-5.
func runSync(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	capture := false
	wait := false
	useStdin := false
	agy := false
	agent := ""
	sid := ""

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--capture":
			capture = true
		case arg == "--wait":
			wait = true
		case arg == "--stdin":
			useStdin = true
		case arg == "--agy":
			agy = true
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "sync", arg+" requires a value")
			}
			index++
			if arg == "--agent" {
				agent = args[index]
			} else {
				sid = args[index]
			}
		case strings.HasPrefix(arg, "--agent="):
			agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--sid="):
			sid = strings.TrimPrefix(arg, "--sid=")
		default:
			return usageError(stderr, "sync", fmt.Sprintf("unknown argument %q", arg))
		}
	}

	var payload hookio.Input
	if useStdin {
		input, err := hookio.ReadInput(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "memctl sync: %v\n", err)
			return ExitUsageError
		}
		payload = input
	}

	if !capture && !wait {
		code := runBackgroundSync(stdout, stderr)
		if agy {
			fmt.Fprint(stdout, "{}")
		}
		return code
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "sync", err)
	}
	service := app.New(cfg)
	ctx := context.Background()

	if capture {
		// The capture commit carries the same Agent/SID provenance as a
		// managed write, using the documented SID priority including the
		// session id in a --stdin Hook payload.
		result, err := service.Capture(ctx,
			hookio.ResolveAgent(agent, os.Getenv),
			hookio.ResolveSID(sid, os.Getenv, payload),
			hookio.ResolveDevice(os.Getenv))
		if err != nil {
			return fail(stderr, "sync", err)
		}
		if result.Commit == "" {
			fmt.Fprintln(stdout, "nothing to capture")
		} else {
			fmt.Fprintf(stdout, "transaction: %s\ncommit: %s\nrevision: %s\nchanged: %s\nsync: %s\n",
				result.TransactionID, result.Commit, result.Revision, strings.Join(result.ChangedFiles, ", "), result.SyncState)
			// A capture is a new local commit, so the automatic path gets the
			// same best-effort background push as a managed write. An explicit
			// --wait below performs the foreground sync instead.
			if !wait && cfg.BackgroundSync {
				_ = service.BeginBackgroundSync()
			}
		}
	}

	if wait {
		background := os.Getenv("MEMCTL_SYNC_BACKGROUND") == "1"
		result, err := service.SyncWithOptions(ctx, app.SyncOptions{Interactive: hasTerminal(), Background: background})
		if !background {
			if agy {
				fmt.Fprint(stdout, "{}")
			} else {
				printSyncResult(stdout, result)
			}
		}
		if err != nil {
			if background {
				// A detached worker must not surface a failure to the hook caller.
				return ExitOK
			}
			return fail(stderr, "sync", err)
		}
	}
	return ExitOK
}

// runBackgroundSync implements the automatic `sync` and `end` Hook path: it
// starts a detached worker and returns immediately. Hook callers always get
// exit code 0 so a busy or offline repository does not block the host.
func runBackgroundSync(stdout, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "sync", err)
	}
	if !cfg.BackgroundSync {
		return ExitOK
	}
	if err := app.New(cfg).BeginBackgroundSync(); err != nil {
		fmt.Fprintf(stderr, "memctl sync: background sync was not started: %v\n", err)
	}
	return ExitOK
}

// runEnd implements `end --agent NAME --sid ID --stdin`, the SessionEnd hook.
func runEnd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	useStdin := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--stdin":
			useStdin = true
		case arg == "--agent" || arg == "--sid":
			if index+1 >= len(args) {
				return usageError(stderr, "end", arg+" requires a value")
			}
			index++
		case strings.HasPrefix(arg, "--agent=") || strings.HasPrefix(arg, "--sid="):
		default:
			return usageError(stderr, "end", fmt.Sprintf("unknown argument %q", arg))
		}
	}
	if useStdin {
		if code := drainHookStdin("end", stdin, stderr); code != ExitOK {
			return code
		}
	}
	return runBackgroundSync(stdout, stderr)
}

func printSyncResult(stdout io.Writer, result app.SyncResult) {
	if result.Branch != "" {
		fmt.Fprintf(stdout, "branch: %s\n", result.Branch)
	}
	if result.Head != "" {
		fmt.Fprintf(stdout, "head: %s\n", result.Head)
	}
	fmt.Fprintf(stdout, "remote: %s\nlocal: %s\n", result.RemoteState, result.LocalState)
	if result.ReasonCode != "" {
		fmt.Fprintf(stdout, "reason: %s\n", result.ReasonCode)
	}
	if result.ConflictID != "" {
		fmt.Fprintf(stdout, "conflict: %s\n", result.ConflictID)
	}
	if result.HasRemote {
		fmt.Fprintf(stdout, "ahead: %d\nbehind: %d\n", result.Ahead, result.Behind)
	}
}

// resolvePayload is the `resolve --apply FILE` input shape.
type resolvePayload struct {
	BaseRevision  string `json:"baseRevision"`
	ResolvedFiles []struct {
		Path       string `json:"path"`
		Action     string `json:"action"`
		RawContent string `json:"rawContent"`
	} `json:"resolvedFiles"`
}

// runResolve implements `resolve [--json]`, `resolve --apply FILE --conflict ID`
// and `resolve --abort --conflict ID`. The view, apply and abort modes are
// mutually exclusive.
func runResolve(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	applyFile := ""
	abort := false
	conflictID := ""

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			asJSON = true
		case arg == "--abort":
			abort = true
		case arg == "--apply" || arg == "--conflict":
			if index+1 >= len(args) {
				return usageError(stderr, "resolve", arg+" requires a value")
			}
			index++
			if arg == "--apply" {
				applyFile = args[index]
			} else {
				conflictID = args[index]
			}
		case strings.HasPrefix(arg, "--apply="):
			applyFile = strings.TrimPrefix(arg, "--apply=")
		case strings.HasPrefix(arg, "--conflict="):
			conflictID = strings.TrimPrefix(arg, "--conflict=")
		default:
			return usageError(stderr, "resolve", fmt.Sprintf("unknown argument %q", arg))
		}
	}
	if abort && applyFile != "" {
		return usageError(stderr, "resolve", "--abort and --apply are mutually exclusive")
	}
	if (abort || applyFile != "") && conflictID == "" {
		return usageError(stderr, "resolve", "--apply and --abort require --conflict ID")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "resolve", err)
	}
	service := app.New(cfg)
	ctx := context.Background()

	if abort {
		result, err := service.ResolveAbort(ctx, conflictID)
		if asJSON {
			if encodeErr := encodeJSON(stdout, resolveOutcome(result)); encodeErr != nil {
				return fail(stderr, "resolve", encodeErr)
			}
		} else {
			printResolveResult(stdout, result)
		}
		if err != nil {
			return fail(stderr, "resolve", err)
		}
		return ExitOK
	}

	if applyFile == "" {
		view, err := service.ResolveStatus(ctx)
		if err != nil {
			return fail(stderr, "resolve", err)
		}
		if asJSON {
			if err := encodeJSON(stdout, view); err != nil {
				return fail(stderr, "resolve", err)
			}
		} else {
			printConflict(stdout, view)
		}
		return ExitOK
	}

	payload, err := readResolvePayload(applyFile)
	if err != nil {
		return fail(stderr, "resolve", err)
	}
	resolved := make([]app.ResolvedFile, 0, len(payload.ResolvedFiles))
	for _, file := range payload.ResolvedFiles {
		resolved = append(resolved, app.ResolvedFile{
			Path:       file.Path,
			Action:     app.ChangeAction(file.Action),
			RawContent: file.RawContent,
		})
	}
	result, err := service.ResolveApply(ctx, hasTerminal(), conflictID, payload.BaseRevision, resolved)
	if asJSON {
		if encodeErr := encodeJSON(stdout, resolveOutcome(result)); encodeErr != nil {
			return fail(stderr, "resolve", encodeErr)
		}
	} else {
		printResolveResult(stdout, result)
	}
	if err != nil {
		return fail(stderr, "resolve", err)
	}
	return ExitOK
}

func readResolvePayload(path string) (resolvePayload, error) {
	if path == "-" {
		return resolvePayload{}, memerr.New(memerr.InvalidArgument, "resolve --apply reads a file path, not stdin")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return resolvePayload{}, memerr.Wrap(memerr.IOFailed, err, "cannot read the resolve payload %s", path)
	}
	if len(data) > 8<<20 {
		return resolvePayload{}, memerr.New(memerr.LimitExceeded, "the resolve payload exceeds the 8 MiB limit")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload resolvePayload
	if err := decoder.Decode(&payload); err != nil {
		return resolvePayload{}, memerr.Wrap(memerr.InvalidArgument, err, "the resolve payload is not valid JSON")
	}
	return payload, nil
}

// resolveOutcome is the JSON shape of a resolve step.
type resolveOutcomePayload struct {
	ConflictID  string            `json:"conflictId,omitempty"`
	Head        string            `json:"head,omitempty"`
	RemoteState string            `json:"remoteState"`
	Conflict    *app.ConflictView `json:"conflict,omitempty"`
}

func resolveOutcome(result app.ResolveResult) resolveOutcomePayload {
	return resolveOutcomePayload{
		ConflictID:  result.ConflictID,
		Head:        result.Head,
		RemoteState: result.RemoteState,
		Conflict:    result.Conflict,
	}
}

func printResolveResult(stdout io.Writer, result app.ResolveResult) {
	if result.Head != "" {
		fmt.Fprintf(stdout, "head: %s\n", result.Head)
	}
	fmt.Fprintf(stdout, "remote: %s\n", result.RemoteState)
	if result.ConflictID != "" {
		fmt.Fprintf(stdout, "conflict: %s\n", result.ConflictID)
	}
}

func printConflict(stdout io.Writer, view app.ConflictView) {
	fmt.Fprintf(stdout, "conflict: %s\nphase: %s\nupstream: %s\nreplay: %s\n", view.ConflictID, view.Phase, view.UpstreamHead, view.ReplayCommit)
	for _, file := range view.Files {
		fmt.Fprintf(stdout, "file: %s (%s) base=%s onto=%s replayed=%s\n",
			file.Path, file.Kind, sideState(file.Base), sideState(file.Onto), sideState(file.Replayed))
	}
}

func sideState(side app.ConflictSideView) string {
	if !side.Exists {
		return "absent"
	}
	if side.Binary {
		return "binary"
	}
	return "present"
}

func encodeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot write JSON output")
	}
	return nil
}

// runRecover implements `recover --status | --rollback`.
func runRecover(args []string, stdout, stderr io.Writer) int {
	showStatus := false
	rollback := false
	for _, arg := range args {
		switch arg {
		case "--status":
			showStatus = true
		case "--rollback":
			rollback = true
		default:
			return usageError(stderr, "recover", fmt.Sprintf("unknown flag %q", arg))
		}
	}
	if showStatus == rollback {
		return usageError(stderr, "recover", "exactly one of --status or --rollback is required")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "recover", err)
	}
	service := app.New(cfg)
	ctx := context.Background()
	var status app.RecoverStatus
	if showStatus {
		status, err = service.RecoverStatus(ctx)
	} else {
		status, err = service.RecoverRollback(ctx)
	}
	printRecoverStatus(stdout, status)
	if err != nil {
		return fail(stderr, "recover", err)
	}
	return ExitOK
}

func printRecoverStatus(stdout io.Writer, status app.RecoverStatus) {
	if len(status.Transactions) == 0 {
		fmt.Fprintln(stdout, "no unfinished transactions")
		return
	}
	for _, tx := range status.Transactions {
		fmt.Fprintf(stdout, "transaction: %s started: %s originalHead: %s\n", tx.ID, tx.StartedAt, tx.OriginalHead)
		switch {
		case tx.Commit != "":
			fmt.Fprintf(stdout, "  resolved by commit: %s\n", tx.Commit)
		case tx.Resolved:
			fmt.Fprintln(stdout, "  rolled back")
		}
		if tx.Reason != "" {
			fmt.Fprintf(stdout, "  reason: %s\n", tx.Reason)
		}
		for _, file := range tx.Files {
			fmt.Fprintf(stdout, "  file: %s %s\n", file.Path, file.State)
		}
	}
}

// readLimited reads stdin up to limit+1 bytes and rejects anything larger
// instead of truncating it.
func readLimited(stdin io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(stdin, limit+1))
	if err != nil {
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot read stdin")
	}
	if int64(len(data)) > limit {
		return nil, memerr.New(memerr.LimitExceeded, "stdin exceeds the %d byte text limit", limit)
	}
	return data, nil
}

func loadConfig() (config.Config, error) {
	return config.Load(os.Getenv)
}
func usageError(stderr io.Writer, cmd, message string) int {
	fmt.Fprintf(stderr, "memctl %s: %s\n", cmd, message)
	return ExitUsageError
}

func fail(stderr io.Writer, cmd string, err error) int {
	if contractErr, ok := memerr.As(err); ok {
		fmt.Fprintf(stderr, "memctl %s: %s: %s\n", cmd, contractErr.Code, contractErr.Message)
	} else {
		fmt.Fprintf(stderr, "memctl %s: %v\n", cmd, err)
	}
	return ExitError
}
