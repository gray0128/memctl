package adapters

import (
	"strings"
	"testing"
)

// TestRegistryHostProtocols pins the host-specific command shapes that broke on
// the real hosts in v0.1.0: Codex and Command Code need the JSON envelope,
// Antigravity reads a named hook from ~/.gemini/config/hooks.json and needs
// its own SessionStart envelope, OpenCode 2 needs a default setup export, and
// dsh installs plugins under a named profile directory.
func TestRegistryHostProtocols(t *testing.T) {
	codex, ok := Lookup("codex")
	if !ok || len(codex.Artifacts) == 0 {
		t.Fatal("codex adapter is missing")
	}
	if command := string(loadAsset(t, codex.Artifacts[0].Asset)); !strings.Contains(command, "start --agent codex --stdin --json") {
		t.Fatalf("codex SessionStart must request the JSON envelope:\n%s", command)
	}

	commandcode, ok := Lookup("commandcode")
	if !ok || len(commandcode.Artifacts) == 0 {
		t.Fatal("commandcode adapter is missing")
	}
	if command := string(loadAsset(t, commandcode.Artifacts[0].Asset)); !strings.Contains(command, "start --agent commandcode --stdin --json") {
		t.Fatalf("commandcode SessionStart must request the JSON envelope:\n%s", command)
	}

	antigravity, ok := Lookup("antigravity")
	if !ok || len(antigravity.Artifacts) != 1 {
		t.Fatal("antigravity adapter is missing")
	}
	artifact := antigravity.Artifacts[0]
	if artifact.PathTemplate != "$HOME/.gemini/config/hooks.json" {
		t.Fatalf("antigravity path = %q, want ~/.gemini/config/hooks.json", artifact.PathTemplate)
	}
	if len(artifact.ContainerPath) != 1 || artifact.ContainerPath[0] != "agent-memory" {
		t.Fatalf("antigravity container = %v, want [agent-memory]", artifact.ContainerPath)
	}
	agyHooks := string(loadAsset(t, artifact.Asset))
	for _, want := range []string{"start --agent antigravity --stdin --agy", "sync --agent antigravity --stdin --agy", `"PostInvocation"`, `"Stop"`} {
		if !strings.Contains(agyHooks, want) {
			t.Fatalf("antigravity template missing %q:\n%s", want, agyHooks)
		}
	}

	opencode, ok := Lookup("opencode")
	if !ok || len(opencode.Artifacts) == 0 {
		t.Fatal("opencode adapter is missing")
	}
	plugin := string(loadAsset(t, opencode.Artifacts[0].Asset))
	for _, want := range []string{"export default", "id: \"agent-memory\"", "setup", "session.execution.started", "session.execution.succeeded"} {
		if !strings.Contains(plugin, want) {
			t.Fatalf("opencode template missing %q:\n%s", want, plugin)
		}
	}

	dsh, ok := Lookup("dsh")
	if !ok || dsh.Plugin == nil {
		t.Fatal("dsh plugin is missing")
	}
	foundProfileGlob := false
	for _, manifest := range dsh.Plugin.Manifests {
		if strings.Contains(manifest, "/profiles/*/node_modules/dsh-hooks-plugin/package.json") {
			foundProfileGlob = true
		}
	}
	if !foundProfileGlob {
		t.Fatalf("dsh manifests do not cover the profile plugin directory: %v", dsh.Plugin.Manifests)
	}
}

// TestRegistryCapabilities checks the AdapterStatus capabilities documented in
// docs/02-domain-models-and-contracts.md section 2.3: every registered adapter
// advertises at least one capability, duplicates are collapsed and unknown
// adapters return an empty list.
func TestRegistryCapabilities(t *testing.T) {
	for _, spec := range Registry() {
		capabilities := Capabilities(spec.Agent)
		if len(capabilities) == 0 {
			t.Fatalf("adapter %s should advertise at least one capability", spec.Agent)
		}
		seen := map[string]bool{}
		for _, capability := range capabilities {
			switch capability {
			case CapabilityHooks, CapabilityConfig, CapabilityPlugin:
			default:
				t.Fatalf("adapter %s has unknown capability %q", spec.Agent, capability)
			}
			if seen[capability] {
				t.Fatalf("adapter %s repeats capability %q", spec.Agent, capability)
			}
			seen[capability] = true
		}
	}

	claude := Capabilities("claude-code")
	if len(claude) != 1 || claude[0] != CapabilityHooks {
		t.Fatalf("claude-code capabilities = %v, want [hooks]", claude)
	}
	codex := Capabilities("codex")
	if len(codex) != 2 || codex[0] != CapabilityHooks || codex[1] != CapabilityConfig {
		t.Fatalf("codex capabilities = %v, want [hooks config]", codex)
	}
	opencode := Capabilities("opencode")
	if len(opencode) != 1 || opencode[0] != CapabilityPlugin {
		t.Fatalf("opencode capabilities = %v, want [plugin]", opencode)
	}
	if unknown := Capabilities("unknown"); len(unknown) != 0 {
		t.Fatalf("unknown adapter capabilities = %v, want empty", unknown)
	}
}
