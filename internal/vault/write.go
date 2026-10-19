package vault

import (
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// MaxRevisionBytes bounds how much of one file the repository revision hashes.
// Managed text is far below this; a larger asset contributes its size and first
// bytes instead of forcing an unbounded read.
const MaxRevisionBytes = 8 << 20

// memctlTempSuffix marks the atomic-write temporary file of one managed path.
const memctlTempSuffix = ".memctl-tmp"

// memctlMoveSuffix marks the intermediate name of a case-only rename. It lives
// only inside one transaction, so a leftover one is a recoverable artifact and
// never a managed memory file.
const memctlMoveSuffix = ".memctl-move"

// FileMeta describes one existing-or-absent managed path without reading the
// whole file into the caller.
type FileMeta struct {
	RelPath  string
	Exists   bool
	IsDir    bool
	Size     int64
	Mode     string // Git file mode, "100644" or "100755"
	FsMode   fs.FileMode
	ModTime  time.Time
	Revision string
	// Links is the hard-link count of an existing regular file. A value
	// greater than 1 means another name shares the content.
	Links uint64
}

// Stat inspects one root-relative path. A missing path is reported with
// Exists=false rather than an error; directories, symlinks and other
// unsupported types are refused.
func (r *Root) Stat(rel string) (FileMeta, error) {
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return FileMeta{}, err
	}
	info, err := r.lstatNoSymlinks(clean)
	if err != nil {
		if contractErr, ok := memerr.As(err); ok && contractErr.Code == memerr.NotFound {
			return FileMeta{RelPath: clean, Exists: false}, nil
		}
		return FileMeta{}, err
	}
	if info.IsDir() {
		return FileMeta{}, memerr.New(memerr.InvalidPath, "path %q is a directory, not a file", clean)
	}
	if !info.Mode().IsRegular() {
		return FileMeta{}, memerr.New(memerr.Unsupported, "path %q is not a regular file", clean)
	}

	meta := FileMeta{
		RelPath: clean,
		Exists:  true,
		Size:    info.Size(),
		Mode:    GitMode(info.Mode()),
		FsMode:  info.Mode().Perm(),
		ModTime: info.ModTime(),
		Links:   hardLinkCount(info),
	}
	content, err := r.readBounded(clean, info.Size())
	if err != nil {
		return FileMeta{}, err
	}
	if int64(len(content)) < info.Size() {
		// A large asset is hashed from its bounded prefix only, so the exact
		// length is folded in as well; two files sharing a prefix must not
		// share a revision.
		meta.Revision = revisionWithLength(meta.Mode, info.Size(), content)
	} else {
		meta.Revision = FileRevision(meta.Mode, content)
	}
	return meta, nil
}

// ListManaged returns every root-relative regular file that is part of the
// managed repository, sorted. It skips `.git`, symbolic links and other
// non-regular entries; it does not follow links out of the repository.
func (r *Root) ListManaged() ([]string, error) {
	var paths []string
	var walk func(dir string) error
	walk = func(dir string) error {
		handle, err := r.root.Open(dir)
		if err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot list %q", dir)
		}
		entries, err := handle.ReadDir(-1)
		handle.Close()
		if err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot list %q", dir)
		}
		for _, entry := range entries {
			name := entry.Name()
			// `.github` holds the Dreaming workflow, which is executed with the
			// remote repository's credentials. It is not managed memory: it is
			// never listed, hashed into a revision or diagnosed as content.
			// See docs/11-github-actions-dreaming.md section 3.2.
			if strings.EqualFold(name, ".git") || strings.EqualFold(name, ".github") {
				continue
			}
			// A crash can leave a hidden atomic-write temporary file or a
			// case-only rename intermediate behind. Neither belongs to a
			// transaction revision and neither is managed memory.
			if strings.HasSuffix(name, memctlTempSuffix) || strings.HasSuffix(name, memctlMoveSuffix) {
				continue
			}
			rel := name
			if dir != "." {
				rel = dir + "/" + name
			}
			mode := entry.Type()
			switch {
			case mode&fs.ModeSymlink != 0:
				continue
			case entry.IsDir():
				if err := walk(rel); err != nil {
					return err
				}
			case mode.IsRegular():
				paths = append(paths, rel)
			}
		}
		return nil
	}
	if err := walk("."); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// WriteAtomic replaces or creates one managed file: it writes and flushes a
// temporary file in the target directory, then renames it over the target. The
// parent directories must already exist; memctl does not invent directory
// structure while holding the repository lock.
func (r *Root) WriteAtomic(rel string, content []byte, mode fs.FileMode) error {
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return err
	}
	parent := path.Dir(clean)
	if parent != "." {
		info, err := r.lstatNoSymlinks(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return memerr.New(memerr.InvalidPath, "parent path %q is not a directory", parent)
		}
	}
	if existing, err := r.lstatNoSymlinks(clean); err == nil {
		if existing.IsDir() {
			return memerr.New(memerr.InvalidPath, "path %q is a directory, not a file", clean)
		}
		if count := hardLinkCount(existing); count > 1 {
			return memerr.New(memerr.Unsupported,
				"path %q has %d hard links; refusing to modify content shared with another name", clean, count)
		}
	}

	tmpName := path.Join(parent, "."+path.Base(clean)+memctlTempSuffix)
	file, err := r.root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create a temporary file for %q", clean)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = r.root.Remove(tmpName)
		return memerr.Wrap(memerr.IOFailed, err, "cannot write %q", clean)
	}
	// The creation mode is masked by the process umask and a leftover
	// temporary file would keep its old mode, so set the target mode explicitly
	// before the rename publishes the file.
	if err := file.Chmod(mode); err != nil {
		file.Close()
		_ = r.root.Remove(tmpName)
		return memerr.Wrap(memerr.IOFailed, err, "cannot set the mode of %q", clean)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = r.root.Remove(tmpName)
		return memerr.Wrap(memerr.IOFailed, err, "cannot flush %q", clean)
	}
	if err := file.Close(); err != nil {
		_ = r.root.Remove(tmpName)
		return memerr.Wrap(memerr.IOFailed, err, "cannot close the temporary file for %q", clean)
	}
	if err := r.root.Rename(tmpName, clean); err != nil {
		_ = r.root.Remove(tmpName)
		return memerr.Wrap(memerr.IOFailed, err, "cannot replace %q", clean)
	}
	return r.syncDir(parent)
}

// RefuseMoveOverwrite rejects a move whose destination already holds a different
// file. A case-only rename is allowed when the destination spelling resolves to
// the same file as the source, which is how a case-insensitive filesystem
// presents it. The source must exist.
func (r *Root) RefuseMoveOverwrite(from, to string) error {
	cleanFrom, err := ValidateRelPath(from)
	if err != nil {
		return err
	}
	cleanTo, err := ValidateRelPath(to)
	if err != nil {
		return err
	}
	if cleanFrom == cleanTo {
		return memerr.New(memerr.InvalidArgument, "source and destination path are identical: %q", cleanFrom)
	}
	fromInfo, err := r.lstatNoSymlinks(cleanFrom)
	if err != nil {
		return err
	}
	toInfo, err := r.lstatNoSymlinks(cleanTo)
	if err != nil {
		if contractErr, ok := memerr.As(err); ok && contractErr.Code == memerr.NotFound {
			return nil
		}
		return err
	}
	if strings.EqualFold(cleanFrom, cleanTo) && os.SameFile(fromInfo, toInfo) {
		return nil
	}
	return memerr.New(memerr.InvalidArgument,
		"destination %q already exists; moving never overwrites an existing file", cleanTo)
}

// MoveIntermediatePath returns the intermediate name a case-only rename uses,
// together with whether that rename is case-only. Recovery uses it to remove a
// name left behind when a crash interrupts the two-step rename.
func MoveIntermediatePath(from, to string) (string, bool) {
	cleanFrom, errFrom := ValidateRelPath(from)
	cleanTo, errTo := ValidateRelPath(to)
	if errFrom != nil || errTo != nil {
		return "", false
	}
	if cleanFrom == cleanTo || !strings.EqualFold(cleanFrom, cleanTo) {
		return "", false
	}
	return path.Join(path.Dir(cleanTo), "."+path.Base(cleanTo)+memctlMoveSuffix), true
}

// RequireParentDir verifies that the parent directory of one root-relative path
// already exists and is a directory. memctl does not create directory structure
// while holding the repository lock, so a new file is refused before any
// recovery record is written.
func (r *Root) RequireParentDir(rel string) error {
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return err
	}
	parent := path.Dir(clean)
	if parent == "." {
		return nil
	}
	info, err := r.lstatNoSymlinks(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return memerr.New(memerr.InvalidPath, "parent path %q is not a directory", parent)
	}
	return nil
}

// RenameFile moves one managed regular file inside the repository without
// overwriting an existing target. A rename that only changes letter case goes
// through a temporary intermediate name, so it also works on a
// case-insensitive filesystem. Parent directories of the destination must
// already exist; memctl does not create directory structure while holding the
// repository lock. See docs/01-architecture-and-modules.md section 3.2.
func (r *Root) RenameFile(from, to string) error {
	cleanFrom, err := ValidateRelPath(from)
	if err != nil {
		return err
	}
	cleanTo, err := ValidateRelPath(to)
	if err != nil {
		return err
	}
	if cleanFrom == cleanTo {
		return memerr.New(memerr.InvalidArgument, "source and destination path are identical: %q", cleanFrom)
	}

	info, err := r.lstatNoSymlinks(cleanFrom)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return memerr.New(memerr.Unsupported, "path %q is not a regular file", cleanFrom)
	}
	if count := hardLinkCount(info); count > 1 {
		return memerr.New(memerr.Unsupported,
			"path %q has %d hard links; refusing to move content shared with another name", cleanFrom, count)
	}

	parent := path.Dir(cleanTo)
	if parent != "." {
		parentInfo, err := r.lstatNoSymlinks(parent)
		if err != nil {
			return err
		}
		if !parentInfo.IsDir() {
			return memerr.New(memerr.InvalidPath, "parent path %q is not a directory", parent)
		}
	}

	// A case-only rename is allowed only when the destination spelling already
	// resolves to the source file, which is how a case-insensitive filesystem
	// presents it. A distinct file at the destination, even one whose name differs
	// only by letter case on a case-sensitive filesystem, is a refused overwrite.
	if err := r.RefuseMoveOverwrite(cleanFrom, cleanTo); err != nil {
		return err
	}
	caseOnly := strings.EqualFold(cleanFrom, cleanTo) && cleanFrom != cleanTo

	if caseOnly {
		intermediate := path.Join(parent, "."+path.Base(cleanTo)+memctlMoveSuffix)
		if _, err := r.lstatNoSymlinks(intermediate); err == nil {
			return memerr.New(memerr.Busy,
				"a leftover rename intermediate %q exists; run `memctl recover --status` before renaming", intermediate)
		} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.NotFound {
			return err
		}
		if err := r.root.Rename(cleanFrom, intermediate); err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot start the case-only rename of %q", cleanFrom)
		}
		if err := r.root.Rename(intermediate, cleanTo); err != nil {
			_ = r.root.Rename(intermediate, cleanFrom)
			return memerr.Wrap(memerr.IOFailed, err, "cannot finish the case-only rename of %q to %q", cleanFrom, cleanTo)
		}
	} else if err := r.root.Rename(cleanFrom, cleanTo); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot move %q to %q", cleanFrom, cleanTo)
	}

	if err := r.syncDir(path.Dir(cleanFrom)); err != nil {
		return err
	}
	if parent != path.Dir(cleanFrom) {
		return r.syncDir(parent)
	}
	return nil
}

// RemoveFile deletes one managed regular file.
func (r *Root) RemoveFile(rel string) error {
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return err
	}
	if _, err := r.lstatNoSymlinks(clean); err != nil {
		return err
	}
	if err := r.root.Remove(clean); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot delete %q", clean)
	}
	return r.syncDir(path.Dir(clean))
}

// syncDir flushes a directory entry change when the platform allows it. The
// rename or remove has already happened atomically, so a flush failure is not
// fatal to the operation.
func (r *Root) syncDir(dir string) error {
	if dir == "" {
		dir = "."
	}
	handle, err := r.root.Open(dir)
	if err != nil {
		return nil
	}
	defer handle.Close()
	_ = handle.Sync()
	return nil
}

func (r *Root) readBounded(rel string, size int64) ([]byte, error) {
	file, err := r.root.Open(rel)
	if err != nil {
		return nil, readError(err, rel)
	}
	defer file.Close()

	limit := int64(MaxRevisionBytes)
	if size > 0 && size < limit {
		limit = size
	}
	content, err := readAllLimit(file, limit)
	if err != nil {
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot read %q", rel)
	}
	return content, nil
}

func readAllLimit(file *os.File, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(file, limit))
}
