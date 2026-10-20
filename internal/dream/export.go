package dream

import (
	"bytes"
	"io/fs"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// baselineFile is the fixed pre-image of one exported memory file. Content is
// held in memory so the proposal diff never re-reads the main repository and no
// exported copy is left on disk. See
// docs/11-github-actions-dreaming.md section 2.1 step 1.
type baselineFile struct {
	content  []byte
	revision string
	gitMode  string
	fsMode   fs.FileMode
}

// baseline is the fixed state a proposal is compared against.
type baseline struct {
	head  string
	files map[string]baselineFile
	// dirs holds every managed directory of the baseline, so a proposal can be
	// refused while it is still a proposal when one of its targets sits under a
	// directory that does not exist. memctl never creates directory structure
	// while holding the repository lock. See
	// docs/01-architecture-and-modules.md section 3.2.
	dirs map[string]bool
	// skipped records managed files that are not exported (binary assets,
	// oversized assets, Git/host configuration). A proposed change to a
	// skipped path is rejected instead of silently clobbering it.
	skipped  map[string]bool
	warnings []string
}

// exportBaseline reads every supported memory file of the locked, clean
// repository into memory. Binary assets, oversized assets and Git/host
// configuration are skipped. The export never follows symbolic links, because
// ListManaged already excludes them and now also skips `.github`.
func exportBaseline(root *vault.Root) (*baseline, error) {
	paths, err := root.ListManaged()
	if err != nil {
		return nil, err
	}
	dirs, err := root.ListDirs()
	if err != nil {
		return nil, err
	}
	base := &baseline{
		files:   make(map[string]baselineFile, len(paths)),
		dirs:    make(map[string]bool, len(dirs)),
		skipped: make(map[string]bool),
	}
	for _, dir := range dirs {
		base.dirs[dir] = true
	}
	for _, rel := range paths {
		if protectedPath(rel) {
			base.skipped[rel] = true
			base.warnings = append(base.warnings, "excluded Git or host configuration from the export: "+rel)
			continue
		}
		file, err := root.ReadFile(rel)
		if err != nil {
			if contract, ok := memerr.As(err); ok {
				switch contract.Code {
				case memerr.Unsupported:
					base.skipped[rel] = true
					base.warnings = append(base.warnings, "excluded a non-text asset from the export: "+rel)
					continue
				case memerr.LimitExceeded:
					base.skipped[rel] = true
					base.warnings = append(base.warnings, "excluded an oversized asset from the export: "+rel)
					continue
				}
			}
			return nil, err
		}
		base.files[rel] = baselineFile{
			content:  []byte(file.RawContent),
			revision: file.FileRevision,
			gitMode:  vault.GitMode(file.Mode),
			fsMode:   file.Mode.Perm(),
		}
	}
	return base, nil
}

// utf8Text reports whether content is valid UTF-8 text without NUL bytes.
func utf8Text(content []byte) bool {
	return utf8.Valid(content) && bytes.IndexByte(content, 0) < 0
}
