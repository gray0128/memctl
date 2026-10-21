package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestStatusEnvReportsServingBinary covers the success branch: the probe
// answers with a version and the tool list, and every registration action is
// derived from the resolved path.
func TestStatusEnvReportsServingBinary(t *testing.T) {
	env := StatusEnv{
		Executable: func() (string, error) { return "/opt/memctl", nil },
		Probe: func(_ context.Context, path, agent string) (ProbeResult, error) {
			if path != "/opt/memctl" || agent != "status" {
				t.Fatalf("probe got path=%q agent=%q", path, agent)
			}
			return ProbeResult{Path: path, Version: "v9.9.9", Tools: []string{"memory_entries", "memory_read", "memory_write"}}, nil
		},
	}

	status, err := env.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Available || status.Version != "v9.9.9" || len(status.Tools) != 3 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(status.Registrations) != 10 {
		t.Fatalf("expected 10 registrations, got %d", len(status.Registrations))
	}
	if got := status.Registrations[0].Command; got != "claude mcp add memctl --scope user -- /opt/memctl mcp --agent claude-code" {
		t.Fatalf("claude-code command = %q", got)
	}
	// Two hosts have no usable CLI command: dsh edits a profile patch file and
	// WorkBuddy has no mcp subcommand at all.
	for _, agent := range []string{"dsh", "workbuddy"} {
		registration, ok := registrationFor(status.Registrations, agent)
		if !ok {
			t.Fatalf("%s registration is missing", agent)
		}
		if registration.Command != "" || registration.Instruction == "" {
			t.Fatalf("%s registration = %+v", agent, registration)
		}
		if agent == "workbuddy" && !strings.Contains(registration.Instruction, "~/.workbuddy/mcp.json") {
			t.Fatalf("workbuddy instruction must name the user-scope mcp.json: %q", registration.Instruction)
		}
	}
	codebuddy, ok := registrationFor(status.Registrations, "codebuddy")
	if !ok || codebuddy.Command != "codebuddy mcp add --scope user memctl -- /opt/memctl mcp --agent codebuddy" {
		t.Fatalf("codebuddy registration = %+v", codebuddy)
	}
}

// TestStatusEnvReportsUnavailableBinary covers the diagnostic branch: a probe
// failure is reported with its reason and no tools, while the registration
// actions stay available so the operator can see what would be registered.
func TestStatusEnvReportsUnavailableBinary(t *testing.T) {
	env := StatusEnv{
		Executable: func() (string, error) { return "/old/memctl", nil },
		Probe: func(context.Context, string, string) (ProbeResult, error) {
			return ProbeResult{}, errors.New(`unknown command "mcp"`)
		},
	}

	status, err := env.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Available || status.Reason == "" || len(status.Tools) != 0 {
		t.Fatalf("unavailable status = %+v", status)
	}
	if len(status.Registrations) != 10 {
		t.Fatalf("registrations should stay visible, got %d", len(status.Registrations))
	}
}

// TestStatusEnvPropagatesExecutableError covers an unresolvable executable,
// which is an environment error rather than an unavailable MCP channel.
func TestStatusEnvPropagatesExecutableError(t *testing.T) {
	env := StatusEnv{Executable: func() (string, error) { return "", errors.New("no executable") }}
	if _, err := env.Status(context.Background()); err == nil {
		t.Fatal("status accepted an unresolvable executable")
	}
}

// registrationFor finds one host's action so the assertions do not depend on
// the order of the registry.
func registrationFor(registrations []Registration, agent string) (Registration, bool) {
	for _, registration := range registrations {
		if registration.Agent == agent {
			return registration, true
		}
	}
	return Registration{}, false
}
