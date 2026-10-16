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
	"github.com/gray0128/memctl/internal/memerr"
)

const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsageError = 2
)

var plannedSubcommands = map[string]string{
	"start":   "synchronize and output session context at session start (planned in A-5)",
	"context": "output session context with per-session sync deduplication (planned in A-5)",
	"pull":    "synchronize repository silently for hook integrations (planned in A-5)",
	"sync":    "trigger background sync or foreground --wait/--capture (planned in A-3/A-4/A-5)",
	"end":     "trigger background sync at session end (planned in A-5)",
	"tag":     "print canonical agent://<device>/<agent>/<sid> source tag (planned in A-5)",
	"wrap":    "run a command wrapped with session identity and pre/post sync (planned in A-5)",
	"write":   "write a managed file within a versioned lock transaction (planned in A-3)",
	"resolve": "inspect, apply, or abort conflict resolution states (planned in A-4)",
	"recover": "inspect or rollback incomplete local transactions (planned in A-3)",
	"ui":      "start the embedded local Web UI server (planned in Stage B)",
	"doctor":  "diagnose repository and state consistency (planned in A-6)",
	"dream":   "generate, inspect, or apply Dreaming memory proposals (planned in Stage D)",
	"install": "inspect, preview, or install agent adapter configurations (planned in Stage C)",
}

const usageText = `Usage:
  memctl <subcommand> [flags] [-- command...]

Available Commands (Stage A-2):
  version              Print memctl version information
  help                 Show this help message
  init [remote-url]    Initialize a local memory repository (--confirm-private)
  read <path>          Read a memory file or structured revision (--json)

Planned Commands (not yet enabled):
  write <path>         Write a memory file with baseline check (--base REV --stdin)
  recover              Inspect or rollback incomplete transactions (--status | --rollback)
  sync                 Synchronize changes (--wait --capture --agent --sid --stdin)
  start                SessionStart hook synchronization and context output
  context              UserPromptSubmit hook synchronization and context output (--once)
  pull                 Silent hook synchronization (--once)
  end                  SessionEnd hook background synchronization
  tag                  Output canonical session source URI (--agent NAME --sid ID)
  wrap                 Wrap external command with pre/post sync (--agent NAME -- <cmd...>)
  resolve              Inspect, apply, or abort rebase conflicts (--json | --apply | --abort)
  doctor               Run repository diagnostics (--json --fix)
  ui                   Launch local Web UI (--port 18420 --no-open --readonly)
  install [agent...]   Manage host adapter configurations (--all --dry-run --status)
  dream                Preview, show, or apply memory organization proposals

Run "memctl help" for usage details.
`

// Run executes the CLI command and returns an exit code per docs/04-ui-and-cli-specification.md.
// version and help never access the memory repository, state directory, Git, or Node.js.
func Run(args []string, _ io.Reader, stdout, stderr io.Writer, version string) int {
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
	}

	if strings.HasPrefix(sub, "-") {
		fmt.Fprintf(stderr, "memctl: unknown flag %q\n", sub)
		fmt.Fprint(stderr, usageText)
		return ExitUsageError
	}

	if desc, ok := plannedSubcommands[sub]; ok {
		fmt.Fprintf(stderr, "memctl %s: UNSUPPORTED: command is not yet implemented (%s)\n", sub, desc)
		return ExitError
	}

	fmt.Fprintf(stderr, "memctl: unknown command %q\n", sub)
	fmt.Fprint(stderr, usageText)
	return ExitUsageError
}

// runInit implements `init [remote-url] --confirm-private`. Stage A-2 only
// creates or reuses a local repository; a remote URL is rejected explicitly
// instead of being silently ignored.
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

	if remoteURL != "" {
		return fail(stderr, "init", memerr.New(memerr.Unsupported,
			"remote connection is not implemented yet (stage A-4); run memctl init without a URL for a local-only repository"))
	}
	if confirmPrivate {
		return usageError(stderr, "init", "--confirm-private requires a remote URL")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail(stderr, "init", err)
	}
	result, err := app.New(cfg).Init(context.Background())
	if err != nil {
		return fail(stderr, "init", err)
	}
	fmt.Fprintf(stdout, "memory repo: %s\nstate: %s\nhead: %s\n", result.RepoPath, result.State, result.Head)
	return ExitOK
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
