package adapters

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gray0128/memctl/internal/assets"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// Adapter status values. See docs/02-domain-models-and-contracts.md section 2.3.
const (
	StatusUnsupported    = "unsupported"
	StatusNotInstalled   = "not_installed"
	StatusConfigured     = "configured"
	StatusVerified       = "verified"
	StatusNeedsAttention = "needs_attention"
)

// Action is the planned or performed operation on one configuration artifact.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
	ActionManual Action = "manual"
)

// ArtifactPlan is one target configuration file in an install plan.
type ArtifactPlan struct {
	Path            string
	Format          Format
	Kind            string
	Action          Action
	CurrentRevision string
	PlannedRevision string
	Note            string
	// Patch is a line preview of the planned change, or the embedded snippet
	// for a manual action.
	Patch string

	mode    os.FileMode
	planned []byte
}

// InstallPlan describes what installing one adapter would do.
type InstallPlan struct {
	Agent         string
	HostFound     bool
	HostCommand   string
	HostVersion   string
	PluginVersion string
	Status        string
	Applicable    bool
	Reason        string
	Artifacts     []ArtifactPlan
}

// Plan builds the install plan for one adapter. It only reads the current
// configuration and never writes a file.
func (env Env) Plan(agent string) (InstallPlan, error) {
	spec, ok := Lookup(agent)
	if !ok {
		return InstallPlan{}, memerr.New(memerr.Unsupported, "adapter %q is not supported", agent)
	}
	return env.planSpec(spec)
}

// PlanAll builds plans for every supported adapter.
func (env Env) PlanAll() ([]InstallPlan, error) {
	plans := make([]InstallPlan, 0, len(registry))
	for _, spec := range Registry() {
		plan, err := env.planSpec(spec)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (env Env) planSpec(spec AdapterSpec) (InstallPlan, error) {
	if env.Home == "" {
		return InstallPlan{}, memerr.New(memerr.InvalidArgument,
			"HOME is not set; cannot resolve the agent configuration paths")
	}
	det := env.detect(spec)
	plan := InstallPlan{
		Agent:         spec.Agent,
		HostFound:     det.hostFound,
		HostCommand:   det.hostCommand,
		HostVersion:   det.hostVersion,
		PluginVersion: det.pluginVersion,
	}

	anyExisting := false
	allSkip := len(spec.Artifacts) > 0
	hasManual := false
	applicable := false
	for _, artifact := range spec.Artifacts {
		planned, err := env.planArtifact(artifact)
		if err != nil {
			return plan, err
		}
		if planned.CurrentRevision != "" {
			anyExisting = true
		}
		switch planned.Action {
		case ActionSkip:
		case ActionManual:
			hasManual = true
			allSkip = false
		default:
			allSkip = false
			applicable = true
		}
		plan.Artifacts = append(plan.Artifacts, planned)
	}

	switch {
	case det.hostUnparsed:
		plan.Status = StatusUnsupported
		plan.Reason = fmt.Sprintf("cannot parse the version reported by %s", det.hostCommand)
		plan.Applicable = false
	case det.pluginMissing():
		plan.Status = StatusNeedsAttention
		plan.Reason = fmt.Sprintf("plugin %s is required but not installed", spec.Plugin.Name)
		plan.Applicable = false
	case hasManual:
		plan.Status = StatusNeedsAttention
		plan.Reason = "one or more configuration files need a manual patch"
		plan.Applicable = applicable
	case !det.hostFound && !anyExisting:
		plan.Status = StatusNotInstalled
		plan.Reason = "host executable not found"
		plan.Applicable = false
	case allSkip:
		plan.Status = StatusConfigured
		plan.Applicable = false
	default:
		plan.Status = StatusNotInstalled
		plan.Reason = "configuration is not installed yet"
		if !det.hostFound {
			plan.Reason = "host executable not found; installing anyway because the configuration exists"
		}
		plan.Applicable = applicable
	}
	return plan, nil
}

func (env Env) planArtifact(artifact ArtifactSpec) (ArtifactPlan, error) {
	path := env.expand(artifact.PathTemplate)
	format := formatForPath(path)
	planned := ArtifactPlan{Path: path, Kind: artifact.Kind, Format: format}

	current, exists, mode, err := env.readCurrent(path)
	if err != nil {
		return planned, err
	}
	planned.mode = mode
	if exists {
		planned.CurrentRevision = vault.FileRevision("100644", current)
	}
	if format == FormatUnsupported {
		planned.Action = ActionManual
		planned.Note = fmt.Sprintf("no supported configuration editor for %q", filepath.Ext(path))
		return planned, nil
	}

	asset, err := assets.AdapterFile(artifact.Asset)
	if err != nil {
		return planned, memerr.Wrap(memerr.IOFailed, err, "cannot read embedded adapter template %s", artifact.Asset)
	}

	if !exists {
		planned.Action = ActionCreate
		planned.PlannedRevision = vault.FileRevision("100644", asset)
		planned.planned = asset
		planned.Patch = previewDiff("", string(asset))
		return planned, nil
	}

	merged, changed, err := mergeContent(format, artifact, asset, current)
	if err != nil {
		if IsManualMerge(err) {
			planned.Action = ActionManual
			planned.Note = err.Error()
			planned.Patch = string(asset)
			return planned, nil
		}
		return planned, err
	}
	if !changed {
		planned.Action = ActionSkip
		return planned, nil
	}
	planned.Action = ActionUpdate
	planned.PlannedRevision = vault.FileRevision("100644", merged)
	planned.planned = merged
	planned.Patch = previewDiff(string(current), string(merged))
	return planned, nil
}

func (env Env) readCurrent(path string) ([]byte, bool, os.FileMode, error) {
	mode := os.FileMode(0o644)
	if env.Stat != nil {
		if info, err := env.Stat(path); err == nil {
			mode = info.Mode().Perm()
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, false, mode, memerr.Wrap(memerr.IOFailed, err, "cannot stat %s", path)
		}
	}
	data, err := env.readFile(path)
	if err != nil {
		if isNotExist(err) {
			return nil, false, mode, nil
		}
		return nil, false, mode, memerr.Wrap(memerr.IOFailed, err, "cannot read %s", path)
	}
	return data, true, mode, nil
}

// ArtifactResult reports what happened to one artifact.
type ArtifactResult struct {
	Path       string
	Action     Action
	BackupPath string
	Note       string
}

// ApplyResult reports the outcome of applying one plan.
type ApplyResult struct {
	Agent   string
	Status  string
	Reason  string
	Applied []ArtifactResult
	Manual  []ArtifactResult
	Skipped []string
}

// AcquireInstallLock serializes a whole install — recovery, planning and every
// file write — so a concurrent install cannot recover another one's journal in
// the middle of its writes, and the single install.json journal is never shared.
// It is independent of the per-path configuration locks.
func (env Env) AcquireInstallLock(ctx context.Context) (*lock.Lock, error) {
	if env.StateDir == "" {
		return nil, memerr.New(memerr.IOFailed, "the adapter install lock needs a state directory")
	}
	dir := filepath.Join(env.StateDir, "adapters")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot create %s", dir)
	}
	return lock.AcquirePath(ctx, filepath.Join(dir, "install.lock"))
}

// Apply writes the plan's create and update artifacts through a per-path lock
// and a same-directory atomic replace. It refuses to run while an earlier
// interrupted install still needs recovery.
func (env Env) Apply(plan InstallPlan) (ApplyResult, error) {
	result := ApplyResult{Agent: plan.Agent, Status: plan.Status, Reason: plan.Reason}
	if !plan.Applicable {
		env.collectUnapplied(plan, &result)
		return result, nil
	}

	journal, err := env.newJournal(plan.Agent)
	if err != nil {
		return result, err
	}
	applied := false
	for index, artifact := range plan.Artifacts {
		switch artifact.Action {
		case ActionManual, ActionSkip:
			continue
		}
		if err := env.applyArtifact(journal, index, artifact, &result); err != nil {
			return result, err
		}
		applied = true
	}
	if applied {
		if err := env.finishJournal(journal); err != nil {
			return result, err
		}
	}
	env.collectUnapplied(plan, &result)

	if spec, ok := Lookup(plan.Agent); ok {
		if refreshed, refreshErr := env.planSpec(spec); refreshErr == nil {
			result.Status = refreshed.Status
			result.Reason = refreshed.Reason
		}
	}
	return result, nil
}

func (env Env) collectUnapplied(plan InstallPlan, result *ApplyResult) {
	for _, artifact := range plan.Artifacts {
		switch artifact.Action {
		case ActionManual:
			result.Manual = append(result.Manual, ArtifactResult{Path: artifact.Path, Action: artifact.Action, Note: artifact.Note})
		case ActionSkip:
			result.Skipped = append(result.Skipped, artifact.Path)
		}
	}
}

func (env Env) applyArtifact(journal *installJournal, index int, artifact ArtifactPlan, result *ApplyResult) error {
	dir := filepath.Dir(artifact.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create %s", dir)
	}
	held, err := lock.AcquirePath(context.Background(), filepath.Join(dir, "."+filepath.Base(artifact.Path)+".memctl.lock"))
	if err != nil {
		return err
	}
	defer held.Release()

	current, exists, mode, err := env.readCurrent(artifact.Path)
	if err != nil {
		return err
	}
	revision := ""
	if exists {
		revision = vault.FileRevision("100644", current)
	}
	if revision != artifact.CurrentRevision {
		return memerr.New(memerr.RevisionMismatch,
			"%s changed since the plan was generated; re-run install to rebuild the plan", artifact.Path)
	}

	record := journalRecord{
		Path:              artifact.Path,
		Existed:           exists,
		Mode:              fmt.Sprintf("%04o", mode),
		InstalledRevision: artifact.PlannedRevision,
		PlannedRevision:   artifact.PlannedRevision,
	}
	if exists {
		backupPath := filepath.Join(env.backupDir(journal.InstallID), fmt.Sprintf("%02d-%s", index, filepath.Base(artifact.Path)))
		if err := writeFileAtomic(backupPath, current, 0o600); err != nil {
			return err
		}
		record.BackupPath = backupPath
	}
	journal.Files = append(journal.Files, record)
	if err := env.saveJournal(journal); err != nil {
		return err
	}

	if err := writeFileAtomic(artifact.Path, artifact.planned, modeOrDefault(artifact.mode)); err != nil {
		return err
	}
	journal.Files[len(journal.Files)-1].Applied = true
	if err := env.saveJournal(journal); err != nil {
		return err
	}

	result.Applied = append(result.Applied, ArtifactResult{Path: artifact.Path, Action: artifact.Action, BackupPath: record.BackupPath})
	return nil
}

func modeOrDefault(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o644
	}
	return mode
}

func previewDiff(before, after string) string {
	var builder strings.Builder
	for _, line := range LineDiff(before, after) {
		builder.WriteByte(line.Kind)
		builder.WriteString(line.Text)
		builder.WriteByte('\n')
	}
	return builder.String()
}

// writeFileAtomic writes data to a temporary file in the target directory,
// flushes it, and renames it into place so a reader never sees a partial file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create %s", dir)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".memctl-*")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create a temporary file in %s", dir)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot write %s", tmpName)
	}
	if err := tmp.Chmod(modeOrDefault(mode)); err != nil {
		tmp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot set the mode of %s", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot flush %s", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot close %s", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot replace %s", path)
	}
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
