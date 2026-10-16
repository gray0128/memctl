// Package assets embeds the default instructions and, from stage C, the
// supported adapter templates so a copied binary stays self-contained. See
// docs/01-architecture-and-modules.md section 5 and
// docs/04-ui-and-cli-specification.md section 6.
//
// The default instructions adapt 脚本化实现参考/INSTRUCTIONS.md from the
// project reference directory to the managed read/write and version-baseline
// rules of this design. The reference directory is never read at runtime.
package assets

import (
	"embed"
	"io/fs"
)

// Instructions is the default instruction block injected at session start.
//
//go:embed INSTRUCTIONS.md
var Instructions string

// Adapters embeds every supported adapter template. The installer copies or
// merges these fragments without reading 脚本化实现参考 at runtime. See
// docs/04-ui-and-cli-specification.md section 6.
//
//go:embed all:adapters
var Adapters embed.FS

// AdapterFile reads one embedded adapter template by its relative path, for
// example "claude-code/hooks.json".
func AdapterFile(name string) ([]byte, error) {
	return fs.ReadFile(Adapters, "adapters/"+name)
}
