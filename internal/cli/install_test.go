package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/adapters"
	dshplugin "github.com/gray0128/memctl/plugins/dsh"
	piplugin "github.com/gray0128/memctl/plugins/pi"
)

// TestPrintPlanShowsManualPatch covers docs/03 section 7: when a configuration
// cannot be merged safely the plan must carry the pasteable snippet. A manual
// patch is not a diff, so its indented lines must not be filtered out.
func TestPrintPlanShowsManualPatch(t *testing.T) {
	var stdout bytes.Buffer
	printPlan(&stdout, adapters.InstallPlan{
		Agent:  "claude-code",
		Status: adapters.StatusNeedsAttention,
		Artifacts: []adapters.ArtifactPlan{{
			Path:   "/tmp/settings.json",
			Format: adapters.FormatJSON,
			Action: adapters.ActionManual,
			Note:   "the hooks value is not a block sequence",
			Patch:  "{\n  \"hooks\": {\n    \"SessionStart\": []\n  }\n}",
		}},
	})
	out := stdout.String()
	for _, want := range []string{`"hooks"`, `"SessionStart"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("manual patch output missing %q:\n%s", want, out)
		}
	}
}

// TestPrintPlanShowsDshInstallHints covers docs/07-dsh-plugin.md section 6.4:
// a dsh adapter without any detected plugin must print the UI, offline and CLI
// entries for memctl-dsh.
func TestPrintPlanShowsDshInstallHints(t *testing.T) {
	var stdout bytes.Buffer
	printPlan(&stdout, adapters.InstallPlan{
		Agent:  "dsh",
		Status: adapters.StatusNeedsAttention,
		Reason: "no dsh plugin detected",
	})
	version, err := dshplugin.Version()
	if err != nil {
		t.Fatalf("plugin version: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"插件 → 添加插件 → memctl-dsh@" + version,
		"memctl dsh plugin-path",
		"dsh plugin --profile <profile> add memctl-dsh@" + version,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dsh install hints missing %q:\n%s", want, out)
		}
	}
}

func TestPrintPlanSkipsDshHintsWhenPluginExists(t *testing.T) {
	var stdout bytes.Buffer
	printPlan(&stdout, adapters.InstallPlan{
		Agent:         "dsh",
		Status:        adapters.StatusConfigured,
		PluginVersion: "0.2.0",
	})
	if strings.Contains(stdout.String(), "插件 → 添加插件") {
		t.Fatalf("configured dsh plan printed install hints:\n%s", stdout.String())
	}
}

// TestPrintPlanShowsPiInstallHints covers docs/08-pi-plugin.md section 7.3: a
// pi adapter without the native plugin prints the npm, offline and verify
// entries and never presents a tarball as an install input.
func TestPrintPlanShowsPiInstallHints(t *testing.T) {
	var stdout bytes.Buffer
	printPlan(&stdout, adapters.InstallPlan{
		Agent:  "pi",
		Status: adapters.StatusNeedsAttention,
		Reason: "no memctl-pi detected",
	})
	version, err := piplugin.Version()
	if err != nil {
		t.Fatalf("plugin version: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"pi install npm:memctl-pi@" + version,
		"memctl pi plugin-dir",
		"pi install <输出的绝对目录>",
		"pi list",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pi install hints missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".tgz") {
		t.Fatalf("pi hints must not present a tarball:\n%s", out)
	}
}

func TestPrintPlanSkipsPiHintsWhenNativePluginExists(t *testing.T) {
	var stdout bytes.Buffer
	printPlan(&stdout, adapters.InstallPlan{
		Agent:                "pi",
		Status:               adapters.StatusConfigured,
		NativePluginDetected: true,
		PluginVersion:        "0.5.0",
	})
	if strings.Contains(stdout.String(), "pi install npm:") {
		t.Fatalf("configured pi plan printed install hints:\n%s", stdout.String())
	}
}
