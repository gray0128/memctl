package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// fakeEnv isolates detection from the real host binaries while using the real
// filesystem for configuration files and state.
func fakeEnv(t *testing.T, hosts, versions map[string]string) (Env, string) {
	t.Helper()
	env := Env{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		LookPath: func(name string) (string, error) {
			if path, ok := hosts[name]; ok {
				return path, nil
			}
			return "", exec.ErrNotFound
		},
		Run: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			if version, ok := versions[path]; ok {
				return []byte(version), nil
			}
			return nil, errors.New("no version")
		},
		ReadFile: os.ReadFile,
		Stat:     os.Stat,
	}
	return env, env.Home
}

func TestPlanAndApplyClaudeCodeIsIdempotent(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"claude": "/usr/bin/claude"}, map[string]string{"/usr/bin/claude": "claude 2.1.0\n"})

	plan, err := env.Plan("claude-code")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !plan.Applicable || plan.Status != StatusNotInstalled || len(plan.Artifacts) != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Artifacts[0].Action != ActionCreate || plan.HostVersion != "2.1.0" {
		t.Fatalf("unexpected artifact plan: %+v", plan.Artifacts[0])
	}

	result, err := env.Apply(plan)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if result.Status != StatusConfigured || len(result.Applied) != 1 {
		t.Fatalf("unexpected apply result: %+v", result)
	}

	target := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target not written: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("target is not valid JSON: %v\n%s", err, data)
	}

	second, err := env.Plan("claude-code")
	if err != nil {
		t.Fatalf("second Plan error: %v", err)
	}
	if second.Status != StatusConfigured || second.Applicable {
		t.Fatalf("second plan must report configured and not applicable: %+v", second)
	}
	secondResult, err := env.Apply(second)
	if err != nil {
		t.Fatalf("second Apply error: %v", err)
	}
	if len(secondResult.Applied) != 0 {
		t.Fatalf("second apply must not rewrite: %+v", secondResult)
	}
}

func TestApplyRejectsStaleConfigurationBaseline(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"claude": "/usr/bin/claude"}, map[string]string{"/usr/bin/claude": "claude 2.1.0\n"})
	plan, err := env.Plan("claude-code")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("{\"model\":\"sonnet\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := env.Apply(plan); err == nil {
		t.Fatal("Apply must reject a configuration changed after the plan")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.RevisionMismatch {
		t.Fatalf("expected REVISION_MISMATCH, got %v", err)
	}
	unchanged, _ := os.ReadFile(target)
	if string(unchanged) != "{\"model\":\"sonnet\"}\n" {
		t.Fatalf("stale apply must not modify the file: %s", unchanged)
	}
}

func TestMissingPluginNeedsAttentionWithoutWriting(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"pi": "/usr/bin/pi"}, map[string]string{"/usr/bin/pi": "pi 1.3.0\n"})
	plan, err := env.Plan("pi")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNeedsAttention || plan.Applicable {
		t.Fatalf("missing plugin must block installation: %+v", plan)
	}
	result, err := env.Apply(plan)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("apply must not write without the plugin: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "hook", "hooks.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("apply wrote the pi config without the plugin: %v", err)
	}
}

func TestPluginPresentAllowsPiInstall(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"pi": "/usr/bin/pi"}, map[string]string{"/usr/bin/pi": "pi 1.3.0\n"})
	manifest := filepath.Join(home, ".pi", "agent", "npm", "node_modules", "pi-yaml-hooks", "package.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"pi-yaml-hooks","version":"2026.9.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := env.Plan("pi")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !plan.Applicable || plan.PluginVersion != "2026.9.0" {
		t.Fatalf("pi plan must be applicable with the plugin: %+v", plan)
	}
	if _, err := env.Apply(plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "hook", "hooks.yaml"))
	if err != nil {
		t.Fatalf("pi config not written: %v", err)
	}
	if string(data) == "" {
		t.Fatal("pi config is empty")
	}
}

func TestHostDetectionStates(t *testing.T) {
	missing, _ := fakeEnv(t, nil, nil)
	plan, err := missing.Plan("claude-code")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.HostFound || plan.Status != StatusNotInstalled || plan.Applicable {
		t.Fatalf("missing host must not be applicable: %+v", plan)
	}

	unparsed, _ := fakeEnv(t, map[string]string{"claude": "/usr/bin/claude"}, map[string]string{"/usr/bin/claude": "no version here\n"})
	plan, err = unparsed.Plan("claude-code")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusUnsupported {
		t.Fatalf("an unparsable version must be unsupported: %+v", plan)
	}
}

func TestUnsupportedAdapterIsRejected(t *testing.T) {
	env, _ := fakeEnv(t, nil, nil)
	if _, err := env.Plan("does-not-exist"); err == nil {
		t.Fatal("unknown adapter must be rejected")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.Unsupported {
		t.Fatalf("expected UNSUPPORTED, got %v", err)
	}
}

func TestRecoveryRestoresOnlyUnchangedFiles(t *testing.T) {
	env, home := fakeEnv(t, nil, nil)
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	installed := []byte("{\"installed\":true}\n")
	if err := os.WriteFile(target, installed, 0o644); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(env.StateDir, "adapters", "backups", "install-test", "00-settings.json")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("{\"original\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installJournal{
		InstallID: "install-test",
		Agent:     "claude-code",
		Files: []journalRecord{{
			Path:              target,
			Existed:           true,
			BackupPath:        backup,
			Mode:              "0644",
			InstalledRevision: vault.FileRevision("100644", installed),
			PlannedRevision:   vault.FileRevision("100644", installed),
			Applied:           true,
		}},
	}
	if err := env.saveJournal(&journal); err != nil {
		t.Fatalf("saveJournal error: %v", err)
	}
	pending, err := env.PendingRecovery()
	if err != nil || !pending {
		t.Fatalf("PendingRecovery = %v, %v", pending, err)
	}

	result, err := env.Recover()
	if err != nil {
		t.Fatalf("Recover error: %v", err)
	}
	if len(result.Restored) != 1 {
		t.Fatalf("expected one restore: %+v", result)
	}
	restored, _ := os.ReadFile(target)
	if string(restored) != "{\"original\":true}\n" {
		t.Fatalf("file was not restored: %s", restored)
	}
	if pending, _ = env.PendingRecovery(); pending {
		t.Fatal("journal must be cleared after recovery")
	}

	// A user edit after the interrupted install must survive recovery.
	if err := os.WriteFile(target, []byte("{\"user\":\"edit\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal.Files[0].Applied = true
	if err := env.saveJournal(&journal); err != nil {
		t.Fatal(err)
	}
	result, err = env.Recover()
	if err != nil {
		t.Fatalf("second Recover error: %v", err)
	}
	if len(result.Skipped) != 1 || len(result.Restored) != 0 {
		t.Fatalf("expected the user edit to be skipped: %+v", result)
	}
	kept, _ := os.ReadFile(target)
	if string(kept) != "{\"user\":\"edit\"}\n" {
		t.Fatalf("recovery overwrote a user edit: %s", kept)
	}
}

func TestRecoveryRemovesFileCreatedByInterruptedInstall(t *testing.T) {
	env, home := fakeEnv(t, nil, nil)
	target := filepath.Join(home, ".commandcode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	installed := []byte("{}\n")
	if err := os.WriteFile(target, installed, 0o644); err != nil {
		t.Fatal(err)
	}
	journal := installJournal{
		InstallID: "install-created",
		Agent:     "commandcode",
		Files: []journalRecord{{
			Path:              target,
			Existed:           false,
			InstalledRevision: vault.FileRevision("100644", installed),
		}},
	}
	if err := env.saveJournal(&journal); err != nil {
		t.Fatal(err)
	}
	result, err := env.Recover()
	if err != nil {
		t.Fatalf("Recover error: %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("expected the created file to be removed: %+v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created file still exists: %v", err)
	}
}

func TestApplyRefusesWhileRecoveryPending(t *testing.T) {
	env, _ := fakeEnv(t, map[string]string{"claude": "/usr/bin/claude"}, map[string]string{"/usr/bin/claude": "claude 2.1.0\n"})
	plan, err := env.Plan("claude-code")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if err := env.saveJournal(&installJournal{InstallID: "pending", Agent: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Apply(plan); err == nil {
		t.Fatal("Apply must refuse while a recovery is pending")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.RecoveryRequired {
		t.Fatalf("expected RECOVERY_REQUIRED, got %v", err)
	}
}

// TestPlanRequiresHome covers the environment boundary: an unresolvable HOME
// must fail instead of expanding every template to a filesystem-root path.
func TestPlanRequiresHome(t *testing.T) {
	env, _ := fakeEnv(t, map[string]string{"claude": "/usr/bin/claude"}, map[string]string{"/usr/bin/claude": "claude 2.1.0\n"})
	env.Home = ""
	_, err := env.Plan("claude-code")
	if err == nil {
		t.Fatal("Plan accepted an empty HOME")
	}
	var contractErr *memerr.Error
	if !errors.As(err, &contractErr) || contractErr.Code != memerr.InvalidArgument {
		t.Fatalf("Plan error = %v, want INVALID_ARGUMENT", err)
	}
}

// TestRecoverSkipsJournalPathsOutsideRegistry covers the recovery boundary: a
// journal record that does not name a registered configuration target is
// reported and left untouched instead of becoming an arbitrary write.
func TestRecoverSkipsJournalPathsOutsideRegistry(t *testing.T) {
	env, _ := fakeEnv(t, map[string]string{}, nil)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := installJournal{
		InstallID: "test-install",
		StartedAt: "2026-01-01T00:00:00Z",
		Agent:     "claude-code",
		Files: []journalRecord{{
			Path:              outside,
			Existed:           true,
			InstalledRevision: vault.FileRevision("100644", []byte("keep me")),
		}},
	}
	if err := env.saveJournal(&journal); err != nil {
		t.Fatal(err)
	}
	result, err := env.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(result.Restored) != 0 || len(result.Removed) != 0 {
		t.Fatalf("recover touched an unregistered path: %+v", result)
	}
	skipped := false
	for _, path := range result.Skipped {
		if path == outside {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("unregistered path was not reported as skipped: %+v", result)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep me" {
		t.Fatalf("unregistered content changed: %q (%v)", got, err)
	}
}

// TestDshPluginProfileDetection covers the real dsh layout: the plugin is
// installed under a named profile directory, not directly under ~/.dsh.
func TestDshPluginProfileDetection(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.1\n"})
	manifest := filepath.Join(home, ".dsh", "profiles", "desktop", "node_modules", "dsh-hooks-plugin", "package.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"dsh-hooks-plugin","version":"0.2.15"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !plan.Applicable || plan.Status != StatusNotInstalled || plan.PluginVersion != "0.2.15" {
		t.Fatalf("dsh plan must detect the profile plugin: %+v", plan)
	}
	if _, err := env.Apply(plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh", "hooks.json")); err != nil {
		t.Fatalf("dsh hooks config was not written: %v", err)
	}
}

func writePluginManifest(t *testing.T, home, name, version string) {
	t.Helper()
	manifest := filepath.Join(home, ".dsh", "profiles", "desktop", "node_modules", name, "package.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"`+name+`","version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDshNativePluginIsConfiguredWithoutHooks(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.2\n"})
	writePluginManifest(t, home, "memctl-dsh", "0.1.2")

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusConfigured || plan.Applicable || len(plan.Artifacts) != 0 {
		t.Fatalf("native plugin plan = %+v, want configured with no artifacts", plan)
	}
	if plan.PluginVersion != "0.1.2" {
		t.Fatalf("PluginVersion = %q, want 0.1.2", plan.PluginVersion)
	}
	if plan.Reason == "" {
		t.Fatal("native plugin plan must explain why hooks.json is not needed")
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("native plugin plan created hooks.json: %v", err)
	}

	result, err := env.Apply(plan)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(result.Applied) != 0 || result.Status != StatusConfigured {
		t.Fatalf("native plugin apply = %+v, want no writes", result)
	}
}

func TestDshNativeAndLegacyPluginsNeedAttention(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.2\n"})
	writePluginManifest(t, home, "memctl-dsh", "0.1.2")
	writePluginManifest(t, home, "dsh-hooks-plugin", "0.2.15")

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNeedsAttention || plan.Applicable || len(plan.Artifacts) != 0 {
		t.Fatalf("coexisting plugin plan = %+v, want needs_attention without artifacts", plan)
	}
	if plan.PluginVersion != "0.1.2" {
		t.Fatalf("PluginVersion = %q, want the native plugin version", plan.PluginVersion)
	}
	if !strings.Contains(plan.Reason, "memctl-dsh") || !strings.Contains(plan.Reason, "dsh-hooks-plugin") {
		t.Fatalf("reason does not name both plugins: %q", plan.Reason)
	}

	result, err := env.Apply(plan)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("coexisting plugins were written: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("coexisting plugins created hooks.json: %v", err)
	}
}

func TestDshWithoutAnyPluginNeedsAttention(t *testing.T) {
	env, _ := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.2\n"})

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNeedsAttention || plan.Applicable || len(plan.Artifacts) != 0 {
		t.Fatalf("missing plugin plan = %+v, want needs_attention without artifacts", plan)
	}
	if plan.PluginVersion != "" {
		t.Fatalf("PluginVersion = %q, want empty", plan.PluginVersion)
	}
	if !strings.Contains(plan.Reason, "memctl-dsh") {
		t.Fatalf("reason does not name the native plugin: %q", plan.Reason)
	}
}

func TestDshOldNativePluginNeedsAttention(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.2\n"})
	writePluginManifest(t, home, "memctl-dsh", "0.0.9")

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNeedsAttention || plan.Applicable {
		t.Fatalf("old native plugin plan = %+v, want needs_attention", plan)
	}
	if !strings.Contains(plan.Reason, "0.1.0") {
		t.Fatalf("reason does not state the minimum version: %q", plan.Reason)
	}
}

func TestDshHostMissingIsNotInstalled(t *testing.T) {
	env, home := fakeEnv(t, nil, nil)
	writePluginManifest(t, home, "memctl-dsh", "0.1.2")

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNotInstalled || plan.Applicable {
		t.Fatalf("missing host plan = %+v, want not_installed", plan)
	}
}

func TestDshLegacyPluginIsMarkedDeprecated(t *testing.T) {
	env, home := fakeEnv(t, map[string]string{"dsh": "/usr/bin/dsh"}, map[string]string{"/usr/bin/dsh": "0.2.1-rc.2\n"})
	writePluginManifest(t, home, "dsh-hooks-plugin", "0.2.15")

	plan, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if plan.Status != StatusNotInstalled || !plan.Applicable {
		t.Fatalf("legacy plan = %+v, want an applicable not_installed plan", plan)
	}
	if !strings.Contains(plan.Reason, "deprecated") {
		t.Fatalf("legacy plan is not marked deprecated: %q", plan.Reason)
	}

	if _, err := env.Apply(plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	configured, err := env.Plan("dsh")
	if err != nil {
		t.Fatalf("second Plan error: %v", err)
	}
	if configured.Status != StatusConfigured || !strings.Contains(configured.Reason, "migrate") {
		t.Fatalf("configured legacy plan = %+v, want a migration reason", configured)
	}
}

func TestVersionLessThan(t *testing.T) {
	tests := []struct {
		version string
		minimum string
		want    bool
	}{
		{"0.0.9", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.2", "0.1.0", false},
		{"v0.1.2", "0.1.0", false},
		{"1.0.0", "0.1.0", false},
		{"0.1.0-rc.1", "0.1.0", false},
		{"unparseable", "0.1.0", false},
	}
	for _, tc := range tests {
		if got := versionLessThan(tc.version, tc.minimum); got != tc.want {
			t.Fatalf("versionLessThan(%q, %q) = %v, want %v", tc.version, tc.minimum, got, tc.want)
		}
	}
}
