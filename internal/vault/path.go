// Package vault owns the repository file boundary and the memory format. This
// file implements root-relative path validation; every repository read or
// write goes through the same rules. See
// docs/01-architecture-and-modules.md section 3.2.
package vault

import (
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// EntryPointName is the required root entry file of a memory repository.
const EntryPointName = "MEMORY.md"

var reservedDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateRelPath normalizes a root-relative path and rejects every alias the
// repository boundary must not accept. The returned path always uses "/".
func ValidateRelPath(path string) (string, error) {
	if path == "" {
		return "", memerr.New(memerr.InvalidPath, "path must not be empty")
	}
	if strings.ContainsRune(path, 0) {
		return "", memerr.New(memerr.InvalidPath, "path must not contain NUL")
	}
	if strings.ContainsRune(path, '\\') {
		return "", memerr.New(memerr.InvalidPath, "path must use / and must not contain backslashes")
	}
	if strings.HasPrefix(path, "/") {
		return "", memerr.New(memerr.InvalidPath, "path must be root-relative, not absolute")
	}
	if isDriveQualified(path) {
		return "", memerr.New(memerr.InvalidPath, "path must not contain a Windows drive letter")
	}

	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if err := validateSegment(segment); err != nil {
			return "", err
		}
	}
	return strings.Join(segments, "/"), nil
}

// IsEntryPoint reports whether rel is the repository root entry file.
func IsEntryPoint(rel string) bool {
	return rel == EntryPointName
}

func validateSegment(segment string) error {
	switch segment {
	case "":
		return memerr.New(memerr.InvalidPath, "path must not contain empty segments")
	case ".", "..":
		return memerr.New(memerr.InvalidPath, "path must not contain %q segments", segment)
	}
	if strings.EqualFold(segment, ".git") {
		return memerr.New(memerr.InvalidPath, "path must not reference Git internals")
	}
	if strings.ContainsRune(segment, ':') {
		return memerr.New(memerr.InvalidPath, "path must not contain alternative data stream syntax")
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return memerr.New(memerr.InvalidPath, "path segment %q has a trailing dot or space alias", segment)
	}
	if name, _, _ := strings.Cut(segment, "."); reservedDeviceNames[strings.ToUpper(name)] {
		return memerr.New(memerr.InvalidPath, "path segment %q is a reserved device name", segment)
	}
	return nil
}

func isDriveQualified(path string) bool {
	if len(path) < 2 {
		return false
	}
	return path[1] == ':' && isASCIIAlpha(path[0])
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
