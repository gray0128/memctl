package adapters

import "testing"

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
