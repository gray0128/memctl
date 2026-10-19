package assets

import (
	"regexp"
	"strings"
	"testing"
)

// TestDreamWorkflowKeepsChecksumContract guards the workflow the Dreaming
// transport installs. The release publishes SHA256SUMS with the asset names as
// written, so the install step must download the binary under that exact name
// and verify it from the same directory; renaming the download breaks
// `sha256sum -c` and the workflow fails before the worker starts (observed on
// the first real run of v0.7.0).
func TestDreamWorkflowKeepsChecksumContract(t *testing.T) {
	workflow := string(DreamWorkflow)
	for _, want := range []string{
		`curl -fsSL "${BASE}/memctl-linux-amd64" -o memctl-linux-amd64`,
		`curl -fsSL "${BASE}/SHA256SUMS" -o SHA256SUMS`,
		`grep "  memctl-linux-amd64$" SHA256SUMS | sha256sum -c -`,
		`chmod +x memctl-linux-amd64`,
		`/tmp/memctl-dream/memctl-linux-amd64 dream --worker`,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("the Dreaming workflow no longer contains %q", want)
		}
	}
	if strings.Contains(workflow, "-o /tmp/memctl\n") {
		t.Error("the workflow downloads the binary under a name that SHA256SUMS does not list")
	}
	if !strings.Contains(workflow, "__MEMCTL_DREAM_VERSION__") {
		t.Error("the workflow lost the version placeholder that --init-workflow replaces")
	}
}

// TestDreamWorkflowTriggersOnRequestBranches guards the trigger the local side
// pushes to and the permissions the worker needs to publish its result.
func TestDreamWorkflowTriggersOnRequestBranches(t *testing.T) {
	workflow := string(DreamWorkflow)
	if !strings.Contains(workflow, "'dream/req/**'") {
		t.Error("the workflow does not trigger on the dream/req branches memctl pushes")
	}
	if !regexp.MustCompile(`(?m)^permissions:\n  contents: write$`).MatchString(workflow) {
		t.Error("the workflow needs contents: write to publish the result branch")
	}
	if !strings.Contains(workflow, "git push origin --delete \"${GITHUB_REF_NAME}\" || true") {
		t.Error("the workflow must consume the request branch immediately")
	}
}
