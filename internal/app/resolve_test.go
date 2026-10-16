package app

import (
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

func resolveConflict() state.Conflict {
	return state.Conflict{
		ConflictID: "cf-test",
		Files: []state.ConflictFile{
			{Path: "MEMORY.md", Kind: syncer.KindText, Replayed: state.ConflictSide{Exists: true, Mode: "100755", BlobID: "a"}},
			{Path: "notes.md", Kind: syncer.KindText, Onto: state.ConflictSide{Exists: true, Mode: "100644", BlobID: "b"}},
		},
	}
}

// TestPlanResolvedValidatesBeforeTouchingTheRepository covers P1-1 and P2-5:
// every static error is refused by the pure planner, so a rejected request can
// never restart the rebase, and the file mode of the replayed side survives.
func TestPlanResolvedValidatesBeforeTouchingTheRepository(t *testing.T) {
	put := func(path, content string) ResolvedFile {
		return ResolvedFile{Path: path, Action: ActionPut, RawContent: content}
	}

	cases := []struct {
		name     string
		conflict state.Conflict
		resolved []ResolvedFile
		want     memerr.Code
	}{
		{
			name: "unsupported conflict type",
			conflict: state.Conflict{ConflictID: "cf-test", Files: []state.ConflictFile{
				{Path: "blob.bin", Kind: syncer.KindBinary},
			}},
			resolved: []ResolvedFile{put("blob.bin", "text")},
			want:     memerr.Unsupported,
		},
		{
			name:     "path outside the conflict",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("other.md", "x"), put("notes.md", "y")},
			want:     memerr.InvalidArgument,
		},
		{
			name:     "duplicate path",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("MEMORY.md", "x"), put("MEMORY.md", "y"), put("notes.md", "z")},
			want:     memerr.InvalidArgument,
		},
		{
			name:     "unknown action",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{{Path: "MEMORY.md", Action: "move"}, put("notes.md", "z")},
			want:     memerr.InvalidArgument,
		},
		{
			name:     "incomplete coverage",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("MEMORY.md", "x")},
			want:     memerr.InvalidArgument,
		},
		{
			name:     "content above the text limit",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("MEMORY.md", strings.Repeat("x", vault.MaxTextFileBytes+1)), put("notes.md", "z")},
			want:     memerr.LimitExceeded,
		},
		{
			name:     "content above the request limit",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("MEMORY.md", strings.Repeat("x", MaxMutationBytes/2+1)), put("notes.md", strings.Repeat("y", MaxMutationBytes/2+1))},
			want:     memerr.LimitExceeded,
		},
		{
			name:     "invalid utf-8 content",
			conflict: resolveConflict(),
			resolved: []ResolvedFile{put("MEMORY.md", string([]byte{0xff, 0xfe})), put("notes.md", "z")},
			want:     memerr.InvalidArgument,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := planResolved(tc.conflict, tc.resolved); err == nil {
				t.Fatal("planResolved accepted an invalid request")
			} else {
				requireContractCode(t, err, tc.want)
			}
		})
	}

	plans, err := planResolved(resolveConflict(), []ResolvedFile{put("MEMORY.md", "#!/bin/sh\ntrue\n"), put("notes.md", "done\n")})
	if err != nil {
		t.Fatalf("planResolved refused a valid request: %v", err)
	}
	if plans[0].mode != 0o755 {
		t.Fatalf("resolved mode = %v, want 0755 from the replayed side", plans[0].mode)
	}
	if plans[1].mode != 0o644 {
		t.Fatalf("resolved mode = %v, want 0644 from the onto side", plans[1].mode)
	}
	if plans[0].content != "#!/bin/sh\ntrue\n" || plans[1].action != ActionPut {
		t.Fatalf("unexpected plans: %+v", plans)
	}
}
