package adapters

import (
	"strings"
	"testing"
)

// TestRegistryHostProtocols pins the host-specific command shapes that broke on
// the real hosts in v0.1.0: Codex and Command Code need the JSON envelope,
// Antigravity reads a named hook from ~/.gemini/config/hooks.json and needs
// its own SessionStart envelope, OpenCode 2 needs a default setup export, and
// dsh installs plugins under a named profile directory. It also pins the
// session-end mappings: Codex SessionEnd/PreCompact and OpenCode
// session.deleted/session.compacted.
func TestRegistryHostProtocols(t *testing.T) {
	codex, ok := Lookup("codex")
	if !ok || len(codex.Artifacts) == 0 {
		t.Fatal("codex adapter is missing")
	}
	codexHooks := string(loadAsset(t, codex.Artifacts[0].Asset))
	for _, want := range []string{
		"start --agent codex --stdin --json",
		`"PreCompact"`,
		"sync --agent codex --stdin",
		`"SessionEnd"`,
		"end --agent codex --stdin",
	} {
		if !strings.Contains(codexHooks, want) {
			t.Fatalf("codex template missing %q:\n%s", want, codexHooks)
		}
	}
	if !strings.Contains(codexHooks, `"timeout": 3`) {
		t.Fatalf("codex SessionEnd must fit the host's 3-second cap:\n%s", codexHooks)
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
	for _, want := range []string{"export default", "id: \"agent-memory\"", "setup", "session.execution.started", "session.execution.succeeded", "session.compacted", "session.deleted", `run("end"`} {
		if !strings.Contains(plugin, want) {
			t.Fatalf("opencode template missing %q:\n%s", want, plugin)
		}
	}

	dsh, ok := Lookup("dsh")
	if !ok || dsh.Plugin == nil || dsh.NativePlugin == nil {
		t.Fatal("dsh legacy or native plugin is missing")
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
	foundNativeProfileGlob := false
	for _, manifest := range dsh.NativePlugin.Manifests {
		if strings.Contains(manifest, "/profiles/*/node_modules/memctl-dsh/package.json") {
			foundNativeProfileGlob = true
		}
	}
	if !foundNativeProfileGlob {
		t.Fatalf("dsh native manifests do not cover the profile plugin directory: %v", dsh.NativePlugin.Manifests)
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
	dsh := Capabilities("dsh")
	if len(dsh) != 2 || dsh[0] != CapabilityHooks || dsh[1] != CapabilityPlugin {
		t.Fatalf("dsh capabilities = %v, want [hooks plugin]", dsh)
	}
	// The advertised capabilities describe the integration path in use, not
	// every path the adapter could take.
	if native := CapabilitiesFor("dsh", true); len(native) != 1 || native[0] != CapabilityPlugin {
		t.Fatalf("native dsh capabilities = %v, want [plugin]", native)
	}
	if legacy := CapabilitiesFor("dsh", false); len(legacy) != 1 || legacy[0] != CapabilityHooks {
		t.Fatalf("legacy dsh capabilities = %v, want [hooks]", legacy)
	}
	if pi := CapabilitiesFor("pi", false); len(pi) != 1 || pi[0] != CapabilityHooks {
		t.Fatalf("legacy pi capabilities = %v, want [hooks]", pi)
	}
	if pi := CapabilitiesFor("pi", true); len(pi) != 1 || pi[0] != CapabilityPlugin {
		t.Fatalf("native pi capabilities = %v, want [plugin]", pi)
	}
	if opencode := CapabilitiesFor("opencode", false); len(opencode) != 1 || opencode[0] != CapabilityPlugin {
		t.Fatalf("opencode capabilities = %v, want [plugin]", opencode)
	}
	if unknown := Capabilities("unknown"); len(unknown) != 0 {
		t.Fatalf("unknown adapter capabilities = %v, want empty", unknown)
	}
}

// TestRegistryCoversCodeBuddyAndWorkBuddy pins the two hosts added for the
// 客户端接入 view: CodeBuddy keeps the Claude-compatible event map in its own
// settings file, and WorkBuddy reads the same map from its state directory.
func TestRegistryCoversCodeBuddyAndWorkBuddy(t *testing.T) {
	codebuddy, ok := Lookup("codebuddy")
	if !ok || len(codebuddy.Artifacts) != 1 {
		t.Fatal("codebuddy adapter is missing")
	}
	codebuddyArtifact := codebuddy.Artifacts[0]
	if codebuddyArtifact.PathTemplate != "$HOME/.codebuddy/settings.json" ||
		len(codebuddyArtifact.ContainerPath) != 1 || codebuddyArtifact.ContainerPath[0] != "hooks" {
		t.Fatalf("codebuddy artifact = %+v", codebuddyArtifact)
	}
	codebuddyHooks := string(loadAsset(t, codebuddyArtifact.Asset))
	for _, want := range []string{
		`"SessionStart"`,
		"start --agent codebuddy --stdin",
		"sync --agent codebuddy --stdin",
		`"SessionEnd"`,
		"end --agent codebuddy --stdin",
	} {
		if !strings.Contains(codebuddyHooks, want) {
			t.Fatalf("codebuddy template missing %q:\n%s", want, codebuddyHooks)
		}
	}

	workbuddy, ok := Lookup("workbuddy")
	if !ok || len(workbuddy.Artifacts) != 1 {
		t.Fatal("workbuddy adapter is missing")
	}
	if len(workbuddy.HostCommands) != 0 || len(workbuddy.HostPaths) != 1 || workbuddy.HostPaths[0] != "$HOME/.workbuddy" {
		t.Fatalf("workbuddy detection = %+v", workbuddy)
	}
	workbuddyArtifact := workbuddy.Artifacts[0]
	if workbuddyArtifact.PathTemplate != "$HOME/.workbuddy/settings.json" {
		t.Fatalf("workbuddy artifact = %+v", workbuddyArtifact)
	}
	workbuddyHooks := string(loadAsset(t, workbuddyArtifact.Asset))
	for _, want := range []string{"start --agent workbuddy --stdin", "sync --agent workbuddy --stdin", "end --agent workbuddy --stdin"} {
		if !strings.Contains(workbuddyHooks, want) {
			t.Fatalf("workbuddy template missing %q:\n%s", want, workbuddyHooks)
		}
	}
}
