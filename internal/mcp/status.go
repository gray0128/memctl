package mcp

import (
	"context"
	"os"
)

// The MCP channel status is shared by `install --status` and the local Web UI.
// Both answer the same two questions without writing anything: does the running
// binary serve the memory tools, and which command registers it in each host?
// See docs/09-mcp-memory-tools.md section 5.3.

// Registration is one host's required action for `memctl mcp`. Command is a
// complete host command, or empty when the host has no equivalent CLI and the
// operator edits its configuration by hand.
type Registration struct {
	Agent       string
	Command     string
	Instruction string
}

// Registrations lists the registration action per adapter. The commands are the
// host's own `mcp add` forms, verified against the installed host CLIs; memctl
// never runs them and never merges host MCP configuration.
func Registrations(memctl string) []Registration {
	server := memctl + " mcp --agent "
	return []Registration{
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

// StatusEnv resolves the binary behind the MCP channel and probes it. A nil
// field uses the real implementation, so the zero value is usable in
// production; tests replace single fields to avoid starting a process.
type StatusEnv struct {
	// Executable returns the absolute memctl path registration commands use.
	Executable func() (string, error)
	// Probe checks one binary's MCP handshake.
	Probe func(ctx context.Context, path, agent string) (ProbeResult, error)
}

// DefaultStatusEnv resolves the running executable and probes it.
func DefaultStatusEnv() StatusEnv {
	return StatusEnv{Executable: os.Executable, Probe: Probe}
}

// Status is the answer of one probe. An unavailable binary is not an error:
// callers report Reason and the registration actions so an operator can see
// the problem, and the UI does not advertise tools that would fail every call.
type Status struct {
	Executable    string
	Version       string
	Tools         []string
	Available     bool
	Reason        string
	Registrations []Registration
}

// Status probes the binary behind the MCP channel. It only resolves the path
// and speaks the protocol: it never reads host configuration, never writes a
// file and never runs a host command.
func (e StatusEnv) Status(ctx context.Context) (Status, error) {
	executable := e.Executable
	if executable == nil {
		executable = os.Executable
	}
	probe := e.Probe
	if probe == nil {
		probe = Probe
	}
	path, err := executable()
	if err != nil {
		return Status{}, err
	}
	status := Status{Executable: path, Registrations: Registrations(path)}
	result, err := probe(ctx, path, "status")
	if err != nil {
		status.Reason = err.Error()
		return status, nil
	}
	status.Available = true
	status.Version = result.Version
	status.Tools = result.Tools
	return status, nil
}
