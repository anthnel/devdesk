// Package themes embeds the theme files that ship with DevDesk, so a shipped
// theme loads without being copied to ~/.devdesk/themes first.
package themes

import "embed"

// FS holds every shipped theme as <name>.json at its root.
//
//go:embed *.json
var FS embed.FS
