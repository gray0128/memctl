// Package adapters installs and inspects the per-host agent configuration that
// wires the local agents to memctl. It reads the host and plugin versions,
// merges managed entries into JSON/JSONC/YAML/TOML configuration without
// reserializing untouched parts, and writes through a same-directory temporary
// file. It never holds the repository lock while touching user configuration.
//
// See docs/03-workflows-and-dataflows.md section 7 and
// docs/01-architecture-and-modules.md section 4.
package adapters

// Artifact kinds.
const (
	// KindMerge merges managed entries into an existing structured file.
	KindMerge = "merge"
	// KindCopy installs a whole file that memctl owns.
	KindCopy = "copy"
)

// Artifact capabilities reported as AdapterStatus.capabilities. See
// docs/02-domain-models-and-contracts.md section 2.3.
const (
	// CapabilityHooks is command hook configuration.
	CapabilityHooks = "hooks"
	// CapabilityConfig is a host feature switch.
	CapabilityConfig = "config"
	// CapabilityPlugin is a file the host loads as a plugin.
	CapabilityPlugin = "plugin"
)

// AdapterSpec describes one supported host. Every path is a template expanded
// against $HOME at detection time so the registry itself stays environment
// independent and testable.
type AdapterSpec struct {
	// Agent is the stable adapter name used on the command line.
	Agent string
	// HostCommands are the executable names tried in order for version
	// detection.
	HostCommands []string
	// Plugin is the optional third-party plugin the host needs for hook
	// support. A host without its plugin is reported as needs_attention
	// instead of configured.
	Plugin *PluginSpec
	// Artifacts are the configuration files memctl manages for this host.
	Artifacts []ArtifactSpec
}

// PluginSpec locates the JSON manifest of a required host plugin.
type PluginSpec struct {
	Name string
	// Manifests are candidate paths; the first readable manifest with a
	// "version" string wins.
	Manifests []string
}

// ArtifactSpec describes one managed configuration file.
type ArtifactSpec struct {
	// PathTemplate is the $HOME-relative target path.
	PathTemplate string
	// Kind is KindMerge or KindCopy.
	Kind string
	// Capability is the AdapterStatus capability this artifact provides:
	// CapabilityHooks, CapabilityConfig or CapabilityPlugin.
	Capability string
	// ContainerPath is the JSON member path that holds the event map, for
	// example ["hooks"]. An empty path means the event names are top-level
	// keys of the file.
	ContainerPath []string
	// Asset is the embedded template path under internal/assets/adapters.
	Asset string
}

// registry is the authoritative adapter list. It covers the candidates in
// docs/05-validation-and-delivery.md section 3; only hosts with an embedded
// template are installable.
var registry = []AdapterSpec{
	{
		Agent:        "claude-code",
		HostCommands: []string{"claude"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.claude/settings.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "claude-code/hooks.json",
		}},
	},
	{
		Agent:        "codex",
		HostCommands: []string{"codex"},
		Artifacts: []ArtifactSpec{
			{
				PathTemplate:  "$HOME/.codex/hooks.json",
				Kind:          KindMerge,
				Capability:    CapabilityHooks,
				ContainerPath: []string{"hooks"},
				Asset:         "codex/hooks.json",
			},
			{
				PathTemplate: "$HOME/.codex/config.toml",
				Kind:         KindMerge,
				Capability:   CapabilityConfig,
				Asset:        "codex/features.toml",
			},
		},
	},
	{
		Agent:        "grok",
		HostCommands: []string{"grok"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.grok/hooks.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "grok/hooks.json",
		}},
	},
	{
		Agent:        "antigravity",
		HostCommands: []string{"antigravity", "agy"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.gemini/settings.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "antigravity/hooks.json",
		}},
	},
	{
		Agent:        "commandcode",
		HostCommands: []string{"commandcode"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.commandcode/settings.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "commandcode/hooks.json",
		}},
	},
	{
		Agent:        "opencode",
		HostCommands: []string{"opencode"},
		Artifacts: []ArtifactSpec{{
			PathTemplate: "$HOME/.config/opencode/plugins/agent-memory.ts",
			Kind:         KindCopy,
			Capability:   CapabilityPlugin,
			Asset:        "opencode/agent-memory.ts",
		}},
	},
	{
		Agent:        "pi",
		HostCommands: []string{"pi"},
		Plugin: &PluginSpec{
			Name:      "pi-yaml-hooks",
			Manifests: []string{"$HOME/.pi/agent/npm/node_modules/pi-yaml-hooks/package.json"},
		},
		Artifacts: []ArtifactSpec{{
			PathTemplate: "$HOME/.pi/agent/hook/hooks.yaml",
			Kind:         KindMerge,
			Capability:   CapabilityHooks,
			Asset:        "pi/hooks.yaml",
		}},
	},
	{
		Agent:        "dsh",
		HostCommands: []string{"dsh"},
		Plugin: &PluginSpec{
			Name: "dsh-hooks-plugin",
			Manifests: []string{
				"$HOME/.dsh/node_modules/dsh-hooks-plugin/package.json",
				"$HOME/.dsh/plugins/dsh-hooks-plugin/package.json",
			},
		},
		Artifacts: []ArtifactSpec{{
			PathTemplate: "$HOME/.dsh/hooks.json",
			Kind:         KindMerge,
			Capability:   CapabilityHooks,
			Asset:        "dsh/hooks.json",
		}},
	},
}

// Registry returns the supported adapters in registration order.
func Registry() []AdapterSpec {
	out := make([]AdapterSpec, len(registry))
	copy(out, registry)
	return out
}

// Lookup returns one adapter by its stable name.
func Lookup(agent string) (AdapterSpec, bool) {
	for _, spec := range registry {
		if spec.Agent == agent {
			return spec, true
		}
	}
	return AdapterSpec{}, false
}

// Capabilities returns the distinct managed capabilities of one adapter in
// registration order. It returns an empty slice for an unknown adapter so the
// JSON response is always a list.
func Capabilities(agent string) []string {
	spec, ok := Lookup(agent)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(spec.Artifacts))
	for _, artifact := range spec.Artifacts {
		if artifact.Capability == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == artifact.Capability {
				found = true
				break
			}
		}
		if !found {
			out = append(out, artifact.Capability)
		}
	}
	return out
}
