package syncer

import (
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
)

// TestClassifyKeepsContractCodes covers P2-2: a contract error that already
// carries a specific code, such as NO_UPSTREAM from the initialization
// protocol, must keep it instead of being flattened to GIT_FAILED.
func TestClassifyKeepsContractCodes(t *testing.T) {
	if got := Classify(memerr.New(memerr.NoUpstream, "no default branch")); got != memerr.NoUpstream {
		t.Fatalf("Classify(NO_UPSTREAM) = %s, want NO_UPSTREAM", got)
	}
	if got := Classify(memerr.New(memerr.RemoteNotApproved, "unconfirmed")); got != memerr.RemoteNotApproved {
		t.Fatalf("Classify(REMOTE_NOT_APPROVED) = %s, want REMOTE_NOT_APPROVED", got)
	}
}
