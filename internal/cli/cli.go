package cli

import (
	"fmt"
	"io"
	"strings"
)

const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsageError = 2
)

var plannedSubcommands = map[string]string{
	"init":    "initialize or connect a memory repository (planned in A-2/A-4)",
	"start":   "synchronize and output session context at session start (planned in A-5)",
	"context": "output session context with per-session sync deduplication (planned in A-5)",
	"pull":    "synchronize repository silently for hook integrations (planned in A-5)",
	"sync":    "trigger background sync or foreground --wait/--capture (planned in A-3/A-4/A-5)",
	"end":     "trigger background sync at session end (planned in A-5)",
	"tag":     "print canonical agent://<device>/<agent>/<sid> source tag (planned in A-5)",
	"wrap":    "run a command wrapped with session identity and pre/post sync (planned in A-5)",
	"read":    "read a managed file and its revision (planned in A-2)",
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

Available Commands (Stage A-1):
  version              Print memctl version information
  help                 Show this help message

Planned Commands (not yet enabled in Stage A-1):
  init [remote-url]    Initialize or connect a memory repository (--confirm-private)
  read <path>          Read a memory file or structured revision (--json)
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
	}

	if strings.HasPrefix(sub, "-") {
		fmt.Fprintf(stderr, "memctl: unknown flag %q\n", sub)
		fmt.Fprint(stderr, usageText)
		return ExitUsageError
	}

	if desc, ok := plannedSubcommands[sub]; ok {
		fmt.Fprintf(stderr, "memctl %s: UNSUPPORTED: command is not yet implemented in Stage A-1 (%s)\n", sub, desc)
		return ExitError
	}

	fmt.Fprintf(stderr, "memctl: unknown command %q\n", sub)
	fmt.Fprint(stderr, usageText)
	return ExitUsageError
}
