package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gray0128/memctl/internal/mcp"
)

// mcpRegistration is one host's required registration action for `memctl mcp`.
type mcpRegistration struct {
	Agent string
	// Command is a complete host command, or empty when the host has no
	// equivalent CLI and the operator edits its configuration by hand.
	Command string
	// Instruction describes the manual step when Command is empty.
	Instruction string
}

// mcpRegistrations lists the registration action per adapter. The commands are
// the host's own `mcp add` forms, verified against the installed host CLIs;
// memctl never runs them and never merges host MCP configuration. See
// docs/09-mcp-memory-tools.md section 5.
func mcpRegistrations(memctl string) []mcpRegistration {
	server := memctl + " mcp --agent "
	return []mcpRegistration{
		{Agent: "claude-code", Command: "claude mcp add memctl --scope user -- " + server + "claude-code"},
		{Agent: "codex", Command: "codex mcp add memctl -- " + server + "codex"},
		{Agent: "grok", Command: "grok mcp add --scope user memctl -- " + server + "grok"},
		{Agent: "antigravity", Command: "agy mcp add --type stdio memctl " + server + "antigravity"},
		{Agent: "commandcode", Command: "commandcode mcp add --scope user memctl " + server + "commandcode"},
		{Agent: "opencode", Command: "opencode mcp add --global memctl -- " + server + "opencode"},
		// pi defaults to codemode exposure, where the model cannot call the
		// tool directly, so the registration must request direct exposure.
		{Agent: "pi", Command: "pi mcp add memctl --exposure direct -- " + server + "pi"},
		{
			Agent:       "dsh",
			Instruction: `add an @deepseek-ai/dsh-mcp-client stdio entry with command "` + memctl + `" and args ["mcp","--agent","dsh"] to the profile's cordis.patch.yml`,
		},
	}
}

// printMCPStatus reports whether the running binary serves the memory tools and
// prints one registration action per host. It never writes a file and never
// runs a host command. A binary that cannot answer tools/list is reported with
// its reason and no registration commands, so an operator does not register a
// server that would fail every call. See M09.
func printMCPStatus(ctx context.Context, stdout io.Writer, memctl string) {
	probe, err := mcp.Probe(ctx, memctl, "status")
	if err != nil {
		fmt.Fprintf(stdout, "mcp: unavailable: %v\n", err)
		fmt.Fprintf(stdout, "  %s does not answer \"mcp\"; reinstall the binary before registering the memory tools\n", memctl)
		return
	}
	status := "serving " + strings.Join(probe.Tools, ", ")
	if probe.Version != "" {
		status += " (" + probe.Version + ")"
	}
	fmt.Fprintf(stdout, "mcp: %s\n", status)
	for _, registration := range mcpRegistrations(memctl) {
		if registration.Command != "" {
			fmt.Fprintf(stdout, "  %s: %s\n", registration.Agent, registration.Command)
			continue
		}
		fmt.Fprintf(stdout, "  %s: %s\n", registration.Agent, registration.Instruction)
	}
}

// selfExecutable returns the running binary path used in registration commands.
func selfExecutable() (string, error) {
	return os.Executable()
}
