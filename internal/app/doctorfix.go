package app

import (
	"context"
	"strings"

	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// DoctorFixRequest is the input of POST /api/v1/doctor:fix and `doctor --fix`.
// IssueCodes selects which diagnostic codes to repair; an empty list selects
// every code with a deterministic repair. BaseRevision optionally binds the
// whole-repository baseline.
type DoctorFixRequest struct {
	IssueCodes   []string
	BaseRevision string
	Agent        string
	SessionID    string
	Device       string
}

// DoctorFixResult is the response of one repair: the report after the attempt
// and, when something changed, the transaction that produced it.
type DoctorFixResult struct {
	Report doctor.Report
	Result *MutationResult
}

// DoctorFix applies the deterministic repairs of the requested diagnostic
// codes through the managed transaction. It refuses a code without a
// deterministic repair with UNSUPPORTED instead of guessing, and it never
// eliminates contradictory facts. The entry file may be missing, so it opens
// the session without requiring the entry point.
func (a *App) DoctorFix(ctx context.Context, req DoctorFixRequest) (DoctorFixResult, error) {
	session, err := a.beginProtected(ctx, false, false)
	if err != nil {
		return DoctorFixResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, req.BaseRevision); err != nil {
		return DoctorFixResult{}, err
	}

	report, err := doctor.Check(ctx, session.repoPath)
	if err != nil {
		return DoctorFixResult{}, err
	}
	plans, err := session.planDoctorFix(report, req.IssueCodes)
	if err != nil {
		return DoctorFixResult{Report: report}, err
	}
	if len(plans) == 0 {
		return DoctorFixResult{Report: report}, nil
	}

	result, err := a.commit(ctx, session, "doctor-fix", MutationRequest{Agent: req.Agent, SessionID: req.SessionID, Device: req.Device}, plans)
	if err != nil {
		return DoctorFixResult{Report: report}, err
	}
	after, err := doctor.Check(ctx, session.repoPath)
	if err != nil {
		return DoctorFixResult{Report: report}, err
	}
	a.finishWrite(session, result)
	return DoctorFixResult{Report: after, Result: &result}, nil
}

// planDoctorFix turns the requested, currently present diagnostic codes into a
// reviewed change list. It rejects any code without a deterministic repair.
func (s *writeSession) planDoctorFix(report doctor.Report, requested []string) ([]plannedChange, error) {
	want := map[string]bool{}
	for _, code := range requested {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if !doctor.AutoFixable(code) {
			return nil, memerr.New(memerr.Unsupported,
				"diagnostic %s has no deterministic repair; fix it by hand", code)
		}
		want[code] = true
	}

	present := map[string]bool{}
	for _, issue := range report.Issues {
		present[issue.Code] = true
	}
	selected := len(want) == 0
	if want[doctor.CodeEntryMissing] && present[doctor.CodeEntryMissing] {
		selected = true
	}
	if !selected || !present[doctor.CodeEntryMissing] {
		return nil, nil
	}

	change := FileChange{
		Path:             vault.EntryPointName,
		ExpectedRevision: vault.AbsentRevision,
		Action:           ActionPut,
		RawContent:       initialMemoryContent,
	}
	return s.plan(MutationRequest{Changes: []FileChange{change}})
}
