package syncer

import (
	"errors"
	"testing"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
)

// TestClassify covers the evidence-based failure classification from
// docs/03-workflows-and-dataflows.md section 4.3.
func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   memerr.Code
	}{
		{"publickey", "git@example.invalid: Permission denied (publickey).", memerr.AuthFailed},
		{"authentication", "fatal: Authentication failed for 'https://example.invalid/repo.git/'", memerr.AuthFailed},
		{"host key", "Host key verification failed.", memerr.AuthFailed},
		{"no username", "fatal: could not read Username for 'https://example.invalid': terminal prompts disabled", memerr.AuthFailed},
		{"dns", "ssh: Could not resolve hostname example.invalid: Name or service not known", memerr.NetworkUnavailable},
		{"refused", "fatal: unable to access 'https://example.invalid/': Failed to connect: Connection refused", memerr.NetworkUnavailable},
		{"non fast forward", "! [rejected]        main -> main (non-fast-forward)", memerr.NonFastForward},
		{"fetch first", "! [rejected] main -> main (fetch first)", memerr.NonFastForward},
		{"unknown", "remote: error: cannot lock ref 'refs/heads/main'", memerr.GitFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &git.Error{Code: memerr.GitFailed, Stderr: tc.stderr}
			if got := Classify(err); got != tc.want {
				t.Fatalf("Classify(%q) = %s, want %s", tc.stderr, got, tc.want)
			}
		})
	}

	// A deadline always wins over prompt-looking stderr because the exchange
	// cannot be proven complete.
	timedOut := &git.Error{Code: memerr.Timeout, TimedOut: true, Stderr: "Permission denied (publickey)."}
	if got := Classify(timedOut); got != memerr.Timeout {
		t.Fatalf("Classify(timeout) = %s, want TIMEOUT", got)
	}
	if got := Classify(errors.New("plain")); got != memerr.GitFailed {
		t.Fatalf("Classify(plain) = %s, want GIT_FAILED", got)
	}
}
