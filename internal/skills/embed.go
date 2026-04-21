package skills

import "embed"

// FS holds all skill/example markdown files baked into the binary at build
// time.  The canonical source lives at internal/skills/data/ (which is what
// .github/copilot/skills symlinks to).
//
//go:embed data
var FS embed.FS
