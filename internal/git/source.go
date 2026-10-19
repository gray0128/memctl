package git

import "strings"

// LocalAgent labels a commit memctl creates on its own behalf rather than for a
// hook session: repository initialization, the managed Dreaming workflow and an
// applied Dreaming proposal. The audit view renders it like any other source, so
// a commit never has to fall back to an unattributed placeholder.
const LocalAgent = "memctl"

// Source is the commit provenance memctl records as trailers in a commit
// message. It is the only input of the read-only audit view's source column;
// the view never infers a source that was not recorded. See
// docs/02-domain-models-and-contracts.md section 2.3.
type Source struct {
	Agent     string
	SessionID string
	Device    string
}

// Trailers renders the non-empty provenance trailer lines. The order is fixed
// so commit messages stay comparable across call sites.
func (s Source) Trailers() []string {
	trailers := make([]string, 0, 3)
	if s.Agent != "" {
		trailers = append(trailers, "Agent: "+s.Agent)
	}
	if s.SessionID != "" {
		trailers = append(trailers, "Session: "+s.SessionID)
	}
	if s.Device != "" {
		trailers = append(trailers, "Device: "+s.Device)
	}
	return trailers
}

// AppendTrailers appends trailer lines as the last paragraph of message, which
// is the layout `git log --format=%b` exposes to the audit view. An empty
// trailer list returns message unchanged.
func AppendTrailers(message string, trailers ...string) string {
	lines := make([]string, 0, len(trailers))
	for _, trailer := range trailers {
		if strings.TrimSpace(trailer) != "" {
			lines = append(lines, trailer)
		}
	}
	if len(lines) == 0 {
		return message
	}
	return strings.TrimRight(message, "\r\n") + "\n\n" + strings.Join(lines, "\n") + "\n"
}
