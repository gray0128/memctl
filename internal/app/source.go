package app

import (
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/hookio"
)

// localSource is the provenance of a commit the application service creates on
// its own behalf when the caller set no source. The agent is always memctl, so
// even a bare library call produces an attributable commit; the device is only
// present when the host resolved one.
func (a *App) localSource() git.Source {
	source := a.commitSource
	source.Agent = git.LocalAgent
	return source
}

// CommitSource is the caller provenance for the commits memctl performs itself
// rather than on behalf of one hook session: repository initialization, the
// managed Dreaming workflow, an applied Dreaming proposal and an audit revert.
// `localSource` substitutes the memctl agent, so a bare library call still
// produces an attributable commit.
func CommitSource(getenv func(string) string) git.Source {
	return git.Source{Agent: git.LocalAgent, Device: hookio.ResolveDevice(getenv)}
}
