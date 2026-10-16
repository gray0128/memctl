package adapters

import (
	"path/filepath"
	"strings"
)

// Format is the configuration format of one artifact.
type Format string

const (
	FormatJSON        Format = "json"
	FormatJSONC       Format = "jsonc"
	FormatYAML        Format = "yaml"
	FormatTOML        Format = "toml"
	FormatText        Format = "text"
	FormatUnsupported Format = "unsupported"
)

// formatForPath infers the configuration format from a target path. JSON and
// JSONC share one comment-tolerant editor; only the reported format differs.
func formatForPath(path string) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON
	case ".jsonc":
		return FormatJSONC
	case ".yaml", ".yml":
		return FormatYAML
	case ".toml":
		return FormatTOML
	case ".ts", ".js", ".mjs", ".cjs", ".md":
		return FormatText
	default:
		return FormatUnsupported
	}
}
