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
	// HostPaths are state-directory templates probed when no host executable
	// is on PATH. A desktop host without a CLI (WorkBuddy) proves installation
	// by its state directory instead of a version command.
	HostPaths []string
	// Plugin is the optional legacy plugin the host needs for hook support.
	// A host without its plugin is reported as needs_attention instead of
	// configured unless a NativePlugin is installed.
	Plugin *PluginSpec
	// NativePlugin is an optional first-party host plugin that replaces the
	// legacy hook configuration. When it is installed memctl manages no
	// configuration for the host and reports configured without artifacts.
	NativePlugin *PluginSpec
	// HostMinVersion is the lowest host version this integration was verified
	// against. It is used for hosts such as pi that have no host-side engine
	// check. An empty value accepts every parseable version.
	HostMinVersion string
	// Artifacts are the configuration files memctl manages for this host.
	Artifacts []ArtifactSpec
}

// PluginSpec locates the JSON manifest of a host plugin.
type PluginSpec struct {
	Name string
	// Manifests are candidate paths; the first readable manifest with a
	// "version" string wins.
	Manifests []string
	// MinVersion is the minimum supported version. An empty value accepts
	// every parseable version.
	MinVersion string
	// DirTemplate is the manifest of the stable local package directory memctl
	// writes under $STATE. It covers offline installs that never enter a
	// package manager directory.
	DirTemplate string
	// SettingsFile is the host's global settings file. Its `packages` array
	// may declare a local package directory as a string or a {source} object;
	// relative paths resolve from the settings file directory.
	SettingsFile string
	// LegacyConfig detects whether the legacy hook configuration still holds
	// this tool's entries. Package presence alone does not prove legacy use.
	LegacyConfig *LegacyConfigSpec
}

// LegacyConfigSpec locates the legacy hook file and its managed entry prefix.
type LegacyConfigSpec struct {
	PathTemplate string
	EntryPrefix  string
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
		// CodeBuddy Code keeps Claude-compatible hook events and exposes the
		// same event map under `hooks` in its user settings file.
		Agent:        "codebuddy",
		HostCommands: []string{"codebuddy", "cbc"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.codebuddy/settings.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "codebuddy/hooks.json",
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
			PathTemplate:  "$HOME/.grok/hooks/memctl.json",
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
			PathTemplate:  "$HOME/.gemini/config/hooks.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"agent-memory"},
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
		// WorkBuddy is a desktop app with no CLI. It runs the CodeBuddy agent
		// core with CODEBUDDY_CONFIG_DIR=$HOME/.workbuddy, so the same `hooks`
		// event map is read from its own settings file.
		Agent:     "workbuddy",
		HostPaths: []string{"$HOME/.workbuddy"},
		Artifacts: []ArtifactSpec{{
			PathTemplate:  "$HOME/.workbuddy/settings.json",
			Kind:          KindMerge,
			Capability:    CapabilityHooks,
			ContainerPath: []string{"hooks"},
			Asset:         "workbuddy/hooks.json",
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
		// pi 0.86.1 is the lowest version with both systemPromptOptions.sections
		// and agent_settled. pi has no host-side engine check, so the adapter
		// reports the gap instead of letting the plugin fail silently.
		HostMinVersion: "0.86.1",
		Plugin: &PluginSpec{
			Name:      "pi-yaml-hooks",
			Manifests: []string{"$HOME/.pi/agent/npm/node_modules/pi-yaml-hooks/package.json"},
		},
		NativePlugin: &PluginSpec{
			Name:       "memctl-pi",
			MinVersion: "0.5.0",
			Manifests: []string{
				"$HOME/.pi/agent/npm/node_modules/memctl-pi/package.json",
			},
			DirTemplate:  "$STATE/pi/package.json",
			SettingsFile: "$HOME/.pi/agent/settings.json",
			LegacyConfig: &LegacyConfigSpec{
				PathTemplate: "$HOME/.pi/agent/hook/hooks.yaml",
				EntryPrefix:  "agent-memory-",
			},
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
				"$HOME/.dsh/profiles/*/node_modules/dsh-hooks-plugin/package.json",
			},
		},
		NativePlugin: &PluginSpec{
			Name:       "memctl-dsh",
			MinVersion: "0.1.0",
			Manifests: []string{
				"$HOME/.dsh/node_modules/memctl-dsh/package.json",
				"$HOME/.dsh/plugins/memctl-dsh/package.json",
				"$HOME/.dsh/profiles/*/node_modules/memctl-dsh/package.json",
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
	out := make([]string, 0, len(spec.Artifacts)+1)
	for _, artifact := range spec.Artifacts {
		if artifact.Capability == "" {
			continue
		}
		if !containsCapability(out, artifact.Capability) {
			out = append(out, artifact.Capability)
		}
	}
	if spec.NativePlugin != nil && !containsCapability(out, CapabilityPlugin) {
		out = append(out, CapabilityPlugin)
	}
	return out
}

// CapabilitiesFor returns the capabilities that describe the integration path
// actually in use. A detected native plugin reports only `plugin`; a host with a
// native plugin but no detected native install reports the legacy path and does
// not advertise `plugin`.
func CapabilitiesFor(agent string, nativeInstalled bool) []string {
	spec, ok := Lookup(agent)
	if !ok {
		return []string{}
	}
	capabilities := Capabilities(agent)
	if spec.NativePlugin == nil {
		return capabilities
	}
	if nativeInstalled {
		return []string{CapabilityPlugin}
	}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability != CapabilityPlugin {
			out = append(out, capability)
		}
	}
	return out
}

func containsCapability(capabilities []string, capability string) bool {
	for _, existing := range capabilities {
		if existing == capability {
			return true
		}
	}
	return false
}
