package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/memerr"
)

// MaxTextFileBytes is the documented per-file text limit from
// docs/02-domain-models-and-contracts.md section 4.1. Larger content is
// rejected instead of being truncated and saved.
const MaxTextFileBytes = 2 << 20

// AbsentRevision is the fixed literal used as the baseline of a file that
// must not exist yet.
const AbsentRevision = "absent"

// Kind classifies a managed file.
type Kind string

const (
	KindEntryPoint Kind = "entrypoint"
	KindMarkdown   Kind = "markdown"
	KindSQL        Kind = "sql"
	KindScript     Kind = "script"
	KindOther      Kind = "other"
)

var scriptExtensions = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".py": true, ".rb": true, ".pl": true, ".js": true, ".mjs": true,
	".cjs": true, ".ts": true, ".ps1": true,
}

// FileRead is the result of reading one managed text file.
type FileRead struct {
	RelPath      string
	FileRevision string
	Kind         Kind
	SizeBytes    int64
	// Mode is the filesystem permission bits, used to preserve the
	// executable bit when the same file is rewritten.
	Mode       fs.FileMode
	Editable   bool
	RawContent string
}

// Root is a repository-bound file accessor. Every path it accepts is
// root-relative; all access goes through an os.Root handle, so a component
// swapped for a link cannot escape the repository.
type Root struct {
	path string
	root *os.Root
}

// OpenRoot binds an accessor to an existing repository directory.
func OpenRoot(repoPath string) (*Root, error) {
	handle, err := os.OpenRoot(repoPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, memerr.Wrap(memerr.NoRepo, err, "memory repository path does not exist: %s", repoPath)
		}
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot open repository root %s", repoPath)
	}
	return &Root{path: repoPath, root: handle}, nil
}

// Close releases the root handle.
func (r *Root) Close() error { return r.root.Close() }

// Path returns the canonical repository root path.
func (r *Root) Path() string { return r.path }

// ReadFile reads one managed text file with the documented bound.
func (r *Root) ReadFile(rel string) (FileRead, error) {
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return FileRead{}, err
	}

	info, err := r.lstatNoSymlinks(clean)
	if err != nil {
		return FileRead{}, err
	}
	if info.IsDir() {
		return FileRead{}, memerr.New(memerr.InvalidPath, "path %q is a directory, not a file", clean)
	}
	if !info.Mode().IsRegular() {
		return FileRead{}, memerr.New(memerr.Unsupported, "path %q is not a regular file", clean)
	}
	if info.Size() > MaxTextFileBytes {
		return FileRead{}, memerr.New(memerr.LimitExceeded,
			"file %q is %d bytes, exceeding the %d byte text limit", clean, info.Size(), MaxTextFileBytes)
	}

	file, err := r.root.Open(clean)
	if err != nil {
		return FileRead{}, readError(err, clean)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, MaxTextFileBytes+1))
	if err != nil {
		return FileRead{}, memerr.Wrap(memerr.IOFailed, err, "cannot read %q", clean)
	}
	if len(content) > MaxTextFileBytes {
		return FileRead{}, memerr.New(memerr.LimitExceeded,
			"file %q exceeds the %d byte text limit", clean, MaxTextFileBytes)
	}
	if !isText(content) {
		return FileRead{}, memerr.New(memerr.Unsupported,
			"file %q is not UTF-8 text; binary assets are synchronized by Git but not read as text", clean)
	}

	mode := GitMode(info.Mode())
	return FileRead{
		RelPath:      clean,
		FileRevision: FileRevision(mode, content),
		Kind:         ClassifyKind(clean),
		SizeBytes:    int64(len(content)),
		Mode:         info.Mode().Perm(),
		Editable:     true,
		RawContent:   string(content),
	}, nil
}

// RepoRevision is the documented whole-repository revision: HEAD, the canonical
// repository path and every managed file's mode and revision, in sorted path
// order, with length-prefixed fields. It is a comparison baseline shared by the
// write transaction and read-only diagnosis, not an authorization token. An
// empty head is the explicit value for a repository without a commit.
func RepoRevision(root *Root, repoPath, head string) (string, error) {
	paths, err := root.ListManaged()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	writeField(hash, []byte(head))
	writeField(hash, []byte(repoPath))
	for _, rel := range paths {
		meta, err := root.Stat(rel)
		if err != nil {
			return "", err
		}
		writeField(hash, []byte(rel))
		writeField(hash, []byte(meta.Mode))
		writeField(hash, []byte(meta.Revision))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// FileRevision is the full SHA-256 of the Git file mode and the raw bytes,
// both length-prefixed so that concatenations cannot collide by construction.
func FileRevision(gitMode string, content []byte) string {
	hash := sha256.New()
	writeField(hash, []byte(gitMode))
	writeField(hash, content)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// GitMode maps a filesystem mode to the mode Git would record.
func GitMode(mode fs.FileMode) string {
	if mode.Perm()&0o111 != 0 {
		return "100755"
	}
	return "100644"
}

// ClassifyKind maps a path to the documented file kind.
func ClassifyKind(rel string) Kind {
	if IsEntryPoint(rel) {
		return KindEntryPoint
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".md", ".markdown":
		return KindMarkdown
	case ".sql":
		return KindSQL
	}
	if scriptExtensions[strings.ToLower(path.Ext(rel))] {
		return KindScript
	}
	return KindOther
}

// lstatNoSymlinks rejects a path when any component is a symbolic link. The
// os.Root handle already prevents escapes; this check additionally refuses
// in-repository links, which are not part of the editable memory.
func (r *Root) lstatNoSymlinks(clean string) (fs.FileInfo, error) {
	segments := strings.Split(clean, "/")
	for i := 1; i < len(segments); i++ {
		prefix := strings.Join(segments[:i], "/")
		info, err := r.root.Lstat(prefix)
		if err != nil {
			return nil, lstatError(err, prefix)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, memerr.New(memerr.Unsupported,
				"path %q traverses the symbolic link %q; links are not readable memory", clean, prefix)
		}
	}

	info, err := r.root.Lstat(clean)
	if err != nil {
		return nil, lstatError(err, clean)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, memerr.New(memerr.Unsupported,
			"path %q is a symbolic link; links are not readable memory", clean)
	}
	return info, nil
}

func lstatError(err error, rel string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return memerr.Wrap(memerr.NotFound, err, "file %q does not exist", rel)
	}
	return memerr.Wrap(memerr.IOFailed, err, "cannot access %q", rel)
}

func readError(err error, rel string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return memerr.Wrap(memerr.NotFound, err, "file %q does not exist", rel)
	}
	return memerr.Wrap(memerr.IOFailed, err, "cannot open %q", rel)
}

func isText(content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 {
		return false
	}
	return utf8.Valid(content)
}

func writeField(w io.Writer, value []byte) {
	var prefix [8]byte
	binary.BigEndian.PutUint64(prefix[:], uint64(len(value)))
	_, _ = w.Write(prefix[:])
	_, _ = w.Write(value)
}
