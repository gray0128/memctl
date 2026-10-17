package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/adapters"
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
