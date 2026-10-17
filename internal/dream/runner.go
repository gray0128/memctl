package dream

import (
	"os/exec"
	"path/filepath"

	"github.com/gray0128/memctl/internal/memerr"
)

// Runner describes one supported Dreaming runner. Binary is either an absolute
// path or a name resolved with PATH. BuildArgs receives the prompt.
//
// The claude and codex definitions are the documented candidates of
// docs/05-validation-and-delivery.md section 3. Their argument shapes were not
// verified against a real host; only a synthetic runner is used in the
// automated tests. A host that has not been verified stays reported as
// unverified rather than being claimed as supported.
type Runner struct {
	Name      string
	Binary    string
	BuildArgs func(prompt string) []string
}

// defaultRunners returns the built-in runner registry. Resolution happens per
// request, so a machine without a given runner reports a targeted error
// instead of failing at construction time.
func defaultRunners() map[string]Runner {
	return map[string]Runner{
		"claude": {
			Name:      "claude",
			Binary:    "claude",
			BuildArgs: func(prompt string) []string { return []string{"-p", prompt} },
		},
		"codex": {
			Name:      "codex",
			Binary:    "codex",
			BuildArgs: func(prompt string) []string { return []string{"exec", prompt} },
		},
	}
}

// resolveRunner finds one runner definition and its executable. A missing
// executable is a targeted UNSUPPORTED error so the caller can tell a missing
// runner apart from a missing sandbox.
func (s *Service) resolveRunner(name string) (Runner, string, error) {
	if name == "" {
		return Runner{}, "", memerr.New(memerr.InvalidArgument, "runner name is required")
	}
	definition, ok := s.Runners[name]
	if !ok {
		return Runner{}, "", memerr.New(memerr.Unsupported,
			"runner %q is not a registered Dreaming runner; supported runners are claude and codex", name)
	}
	binary := definition.Binary
	if binary == "" {
		binary = definition.Name
	}
	if !filepath.IsAbs(binary) {
		resolved, err := exec.LookPath(binary)
		if err != nil {
			return Runner{}, "", memerr.Wrap(memerr.Unsupported, err,
				"runner %q is not installed or not on PATH; install it or choose another runner", name)
		}
		binary = resolved
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return Runner{}, "", memerr.Wrap(memerr.IOFailed, err, "cannot resolve runner %q path", name)
	}
	return definition, absolute, nil
}

// prompt is the documented instruction block passed to a runner. The runner
// edits the memory files in its working directory in place; memctl computes the
// change set from the fixed baseline afterwards and never trusts the runner's
// own description of what it changed.
const prompt = `You are organizing a personal memory repository that is checked out read-only-except-for-this-directory.

The current directory contains the memory files. MEMORY.md is the entry point and other Markdown files are linked from it with [[name]] links.

Task:
- Reorganize the memory: merge duplicates, improve headings, group related facts, fix obviously stale or contradictory entries, and keep the root-relative [[link]] syntax valid.
- Preserve every fact you are not confident about; do not invent new facts.
- Edit the files in place. Do not create binary files, symlinks, or directories outside this working directory.
- Do not run destructive commands and do not attempt to modify anything outside this directory.

When you are done, stop. memctl will compute the difference itself.`
