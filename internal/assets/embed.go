// Package assets embeds the default instructions and, from stage C, the
// supported adapter templates so a copied binary stays self-contained. See
// docs/01-architecture-and-modules.md section 5 and
// docs/04-ui-and-cli-specification.md section 6.
//
// The default instructions adapt 脚本化实现参考/INSTRUCTIONS.md from the
// project reference directory to the managed read/write and version-baseline
// rules of this design. The reference directory is never read at runtime.
package assets

import _ "embed"

// Instructions is the default instruction block injected at session start.
//
//go:embed INSTRUCTIONS.md
var Instructions string
