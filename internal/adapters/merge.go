package adapters

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrManualMerge marks a file that memctl will not rewrite automatically. The
// install plan reports the embedded snippet as a manual patch instead of
// reserializing the whole file. See docs/03-workflows-and-dataflows.md
// section 7.
var ErrManualMerge = errors.New("manual merge required")

type manualMergeError struct {
	message string
}

func (e *manualMergeError) Error() string { return e.message }
func (e *manualMergeError) Is(target error) bool {
	return target == ErrManualMerge
}

func errManualMerge(format string, args ...any) error {
	return &manualMergeError{message: fmt.Sprintf(format, args...)}
}

// IsManualMerge reports whether err asks for a manual patch.
func IsManualMerge(err error) bool { return errors.Is(err, ErrManualMerge) }

func errInvalidTemplate(kind string, err error) error {
	if err == nil {
		return fmt.Errorf("embedded %s adapter template is malformed", kind)
	}
	return fmt.Errorf("embedded %s adapter template is malformed: %w", kind, err)
}

// mergeContent merges the embedded template into the current file content.
// changed is false when the file already matches the template.
func mergeContent(format Format, spec ArtifactSpec, asset, src []byte) ([]byte, bool, error) {
	switch format {
	case FormatJSON, FormatJSONC:
		return mergeJSONContent(src, spec, asset)
	case FormatYAML:
		return mergeYAMLContent(src, asset)
	case FormatTOML:
		return mergeTOMLContent(src, asset)
	case FormatText:
		return mergeCopyContent(asset, src)
	default:
		return nil, false, errManualMerge("the %s format is not supported for automatic merging", format)
	}
}

// mergeCopyContent installs a whole file that memctl owns. An existing file
// that does not mention memctl belongs to the user and is left untouched.
func mergeCopyContent(asset, src []byte) ([]byte, bool, error) {
	if bytes.Equal(src, asset) {
		return src, false, nil
	}
	if len(src) > 0 && !bytes.Contains(src, []byte("memctl")) {
		return nil, false, errManualMerge("the target file already exists and is not managed by memctl")
	}
	return asset, true, nil
}
