package dream

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// baselineFile is the fixed pre-image of one exported memory file. Content is
// held in memory so the proposal diff never re-reads the main repository.
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
	// skipped records managed files that are not exported (binary assets,
	// oversized assets, Git/host configuration). A proposed change to a
	// skipped path is rejected instead of silently clobbering it.
	skipped  map[string]bool
	warnings []string
}

// repoDir returns the exported memory directory a runner works in.
func repoDir(workDir string) string { return filepath.Join(workDir, "repo") }

// exportBaseline copies every supported memory file of the locked, clean
// repository into the temporary work directory. Binary assets, oversized
// assets and Git/host configuration are skipped. The export never follows
// symbolic links and never copies `.git`, because ListManaged already excludes
// them. See docs/03-workflows-and-dataflows.md section 6 step 1.
func exportBaseline(root *vault.Root, workDir string) (*baseline, error) {
	paths, err := root.ListManaged()
	if err != nil {
		return nil, err
	}
	base := &baseline{
		files:   make(map[string]baselineFile, len(paths)),
		skipped: make(map[string]bool),
	}
	target := repoDir(workDir)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot create the export directory %s", target)
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
		destination := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return nil, memerr.Wrap(memerr.IOFailed, err, "cannot create %s", filepath.Dir(destination))
		}
		if err := os.WriteFile(destination, []byte(file.RawContent), file.Mode.Perm()); err != nil {
			return nil, memerr.Wrap(memerr.IOFailed, err, "cannot export %s", rel)
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
