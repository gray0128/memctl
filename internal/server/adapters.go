package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gray0128/memctl/internal/adapters"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file implements the adapter routes of stage C-3 subtask 2. The HTTP
// layer only validates the request shape, turns the client's previewed config
// revisions into a baseline and maps contract errors; detection, merging,
// locking and recovery stay in internal/adapters, so the CLI and the UI share
// one installer.

// adapterStatusDTO is the JSON shape of AdapterStatus. See
// docs/02-domain-models-and-contracts.md section 2.3.
type adapterStatusDTO struct {
	Agent         string   `json:"agent"`
	HostVersion   string   `json:"hostVersion,omitempty"`
	PluginVersion string   `json:"pluginVersion,omitempty"`
	ConfigPaths   []string `json:"configPaths"`
	Status        string   `json:"status"`
	Capabilities  []string `json:"capabilities"`
	Reason        string   `json:"reason,omitempty"`
}

// adapterArtifactDTO is one ArtifactPlan of an AdapterInstallPlan.
type adapterArtifactDTO struct {
	Path            string `json:"path"`
	Format          string `json:"format"`
	Action          string `json:"action"`
	CurrentRevision string `json:"currentRevision,omitempty"`
	PlannedRevision string `json:"plannedRevision,omitempty"`
	Note            string `json:"note,omitempty"`
	Patch           string `json:"patch,omitempty"`
}

// adapterPlanDTO is the JSON shape of AdapterInstallPlan.
type adapterPlanDTO struct {
	Agent         string               `json:"agent"`
	HostVersion   string               `json:"hostVersion,omitempty"`
	PluginVersion string               `json:"pluginVersion,omitempty"`
	Status        string               `json:"status"`
	Applicable    bool                 `json:"applicable"`
	Reason        string               `json:"reason,omitempty"`
	Artifacts     []adapterArtifactDTO `json:"artifacts"`
}

// adapterChangeDTO reports one applied or manual artifact.
type adapterChangeDTO struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	BackupPath string `json:"backupPath,omitempty"`
	Note       string `json:"note,omitempty"`
}

// adapterRecoveryDTO reports a recovered interrupted install.
type adapterRecoveryDTO struct {
	Recovered bool     `json:"recovered"`
	Restored  []string `json:"restored"`
	Removed   []string `json:"removed"`
	Skipped   []string `json:"skipped"`
}

// adapterInstallResultDTO is the response of the install route.
type adapterInstallResultDTO struct {
	DryRun          bool                `json:"dryRun"`
	Status          adapterStatusDTO    `json:"status"`
	Plan            adapterPlanDTO      `json:"plan"`
	Applied         []adapterChangeDTO  `json:"applied"`
	Manual          []adapterChangeDTO  `json:"manual"`
	Skipped         []string            `json:"skipped"`
	Recovery        *adapterRecoveryDTO `json:"recovery,omitempty"`
	PendingRecovery bool                `json:"pendingRecovery"`
}

// adapterInstallBody is the POST /api/v1/adapters/{agent}/install request.
// expectedConfigRevisions maps an absolute configuration path to the
// fileRevision the client previewed.
type adapterInstallBody struct {
	DryRun                  bool              `json:"dryRun"`
	ExpectedConfigRevisions map[string]string `json:"expectedConfigRevisions"`
}

// handleAdapters lists the detection status of every supported adapter. It only
// probes and previews; it never writes a configuration file.
func (s *Server) handleAdapters(w http.ResponseWriter, r *http.Request) {
	plans, err := s.adapters.PlanAll()
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	statuses := make([]adapterStatusDTO, 0, len(plans))
	for _, plan := range plans {
		statuses = append(statuses, toAdapterStatusDTO(plan))
	}
	writeJSON(w, map[string]any{"adapters": statuses})
}

// handleAdapterInstall previews or applies one adapter's configuration.
//
// A dry run only returns the plan and does not recover an interrupted install.
// A real install first recovers the interrupted install, rebuilds the plan from
// the current configuration and requires expectedConfigRevisions to match every
// existing target, so a file the user changed after the preview is never
// overwritten. A recovered install has already replaced the previewed files
// with their pre-install content, so that request skips the (now meaningless)
// baseline check and reports the recovery in the response.
func (s *Server) handleAdapterInstall(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	var body adapterInstallBody
	if err := decodeOptionalBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	if _, ok := adapters.Lookup(agent); !ok {
		s.writeAppError(w, memerr.New(memerr.Unsupported, "adapter %q is not supported", agent))
		return
	}

	if body.DryRun {
		plan, err := s.adapters.Plan(agent)
		if err != nil {
			s.writeAppError(w, err)
			return
		}
		pending, err := s.adapters.PendingRecovery()
		if err != nil {
			s.writeAppError(w, err)
			return
		}
		s.writeAdapterResult(w, plan, adapters.ApplyResult{
			Agent:  plan.Agent,
			Status: plan.Status,
			Reason: plan.Reason,
		}, nil, true, pending)
		return
	}

	// A real install serializes recovery, planning and every write with the
	// install-level lock, so a concurrent install cannot recover this journal or
	// share the same journal file mid-write.
	held, err := s.adapters.AcquireInstallLock(r.Context())
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	defer held.Release()

	recovery, err := s.adapters.Recover()
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	plan, err := s.adapters.Plan(agent)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	// A recovered interrupted install replaced the previewed files with the
	// pre-install content, so the previewed baseline no longer describes the
	// target. The plan was just rebuilt from the recovered content; apply it
	// directly and report the recovery in the response.
	if !recovery.Recovered {
		if err := checkAdapterBaseline(plan, body.ExpectedConfigRevisions); err != nil {
			s.writeAppError(w, err)
			return
		}
	}
	result, err := s.adapters.Apply(plan)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	s.writeAdapterResult(w, plan, result, &recovery, false, false)
}

// checkAdapterBaseline requires one matching revision for every target that
// already exists. A missing revision is a precondition problem; a different
// revision means the file changed after the preview.
func checkAdapterBaseline(plan adapters.InstallPlan, expected map[string]string) error {
	for _, artifact := range plan.Artifacts {
		if artifact.CurrentRevision == "" {
			continue
		}
		provided, ok := expected[artifact.Path]
		if !ok || strings.TrimSpace(provided) == "" {
			return memerr.New(memerr.PreconditionRequired,
				"installing %s requires expectedConfigRevisions for the existing configuration %s",
				plan.Agent, artifact.Path)
		}
		if provided != artifact.CurrentRevision {
			return memerr.New(memerr.RevisionMismatch,
				"%s changed since the preview; re-run the preview to rebuild the install plan", artifact.Path)
		}
	}
	return nil
}

func (s *Server) writeAdapterResult(
	w http.ResponseWriter,
	plan adapters.InstallPlan,
	result adapters.ApplyResult,
	recovery *adapters.RecoveryResult,
	dryRun bool,
	pending bool,
) {
	status := toAdapterStatusDTO(plan)
	status.Status = result.Status
	status.Reason = result.Reason
	payload := adapterInstallResultDTO{
		DryRun:          dryRun,
		Status:          status,
		Plan:            toAdapterPlanDTO(plan),
		Applied:         adapterChanges(result.Applied),
		Manual:          adapterChanges(result.Manual),
		Skipped:         stringsOrEmpty(result.Skipped),
		PendingRecovery: pending,
	}
	if recovery != nil && recovery.Recovered {
		payload.Recovery = &adapterRecoveryDTO{
			Recovered: recovery.Recovered,
			Restored:  stringsOrEmpty(recovery.Restored),
			Removed:   stringsOrEmpty(recovery.Removed),
			Skipped:   stringsOrEmpty(recovery.Skipped),
		}
	}
	writeJSON(w, payload)
}

func toAdapterStatusDTO(plan adapters.InstallPlan) adapterStatusDTO {
	paths := make([]string, 0, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		paths = append(paths, artifact.Path)
	}
	return adapterStatusDTO{
		Agent:         plan.Agent,
		HostVersion:   plan.HostVersion,
		PluginVersion: plan.PluginVersion,
		ConfigPaths:   paths,
		Status:        plan.Status,
		Capabilities:  adapters.Capabilities(plan.Agent),
		Reason:        plan.Reason,
	}
}

func toAdapterPlanDTO(plan adapters.InstallPlan) adapterPlanDTO {
	artifacts := make([]adapterArtifactDTO, 0, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		artifacts = append(artifacts, adapterArtifactDTO{
			Path:            artifact.Path,
			Format:          string(artifact.Format),
			Action:          string(artifact.Action),
			CurrentRevision: artifact.CurrentRevision,
			PlannedRevision: artifact.PlannedRevision,
			Note:            artifact.Note,
			Patch:           artifact.Patch,
		})
	}
	return adapterPlanDTO{
		Agent:         plan.Agent,
		HostVersion:   plan.HostVersion,
		PluginVersion: plan.PluginVersion,
		Status:        plan.Status,
		Applicable:    plan.Applicable,
		Reason:        plan.Reason,
		Artifacts:     artifacts,
	}
}

func adapterChanges(results []adapters.ArtifactResult) []adapterChangeDTO {
	changes := make([]adapterChangeDTO, 0, len(results))
	for _, result := range results {
		changes = append(changes, adapterChangeDTO{
			Path:       result.Path,
			Action:     string(result.Action),
			BackupPath: result.BackupPath,
			Note:       result.Note,
		})
	}
	return changes
}

func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// decodeOptionalBody reads a bounded JSON body for a route whose body is
// optional. It still rejects undeclared fields, malformed JSON and trailing
// bytes after the single JSON value.
func decodeOptionalBody(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return memerr.New(memerr.LimitExceeded, "the request body exceeds the %d byte limit", tooLarge.Limit)
		}
		return memerr.Wrap(memerr.InvalidArgument, err, "the request body is not valid JSON for this route")
	}
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return memerr.New(memerr.LimitExceeded, "the request body exceeds the %d byte limit", tooLarge.Limit)
		}
	}
	return memerr.New(memerr.InvalidArgument, "the request body must contain exactly one JSON value")
}
