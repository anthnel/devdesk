// Package fileicon answers one question: which glyph names this file.
//
// The rule is internal/viewer/detect.go's, deliberately: the **basename is
// consulted before the extension**, because `Dockerfile` carries no extension
// at all and `Dockerfile.dev` carries `.dev`, which is in no table — an
// extension-first lookup would settle it as a plain file before the name ever
// got a say. `.gitlab-ci.yml` falls out of the same rule for free.
//
// Nothing is sniffed from content. A glyph is a claim about what a file *is*,
// and "this looks like YAML" is not a decidable question — the registry
// `provider` field is the precedent, declared and never inferred.
//
// It is not the viewer's Kind table. There are a dozen Kinds, enough to pick a
// lexer; there are dozens of icons, and several share a Kind — `.js`, `.ts`,
// `.py` and `.rs` would all be "text". Two questions about the same file, so
// two tables.
//
// The glyphs live here rather than in theme/icons.go, and that is the one place
// this package departs from the ForgeIcon precedent. There, the glyph is in
// theme and the vocabulary in internal/forge because a domain package must not
// import the UI. Here both halves are UI, and a table whose key is in one file
// and whose value is in another cannot be read one entry at a time. theme keeps
// the icons the application uses *by name* — IconDirectory, IconFile,
// IconGitBranch — and this package borrows them for what it cannot name.
package fileicon

import (
	"path/filepath"
	"strings"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The glyphs this package adds, written as escapes rather than as characters —
// icons.go's convention, and the only form in which a codepoint can be read and
// checked rather than trusted to have survived a copy.
//
// They are all nf-md (the U+F0000 plane), which is the family icons.go already
// leans on, and the set is deliberately small: a wrong codepoint renders as a
// tofu box, so every entry here is one more thing that has to be looked at on a
// real terminal. Anything not worth that check falls back to IconCodeFile or
// IconFile below rather than guessing.
const (
	iconMarkdown = "\U000F0354" // 󰍔 nf-md-language_markdown
	iconShell    = "\U000F018D" // 󰆍 nf-md-console
	// The four language_* glyphs below are one contiguous MDI run, in
	// alphabetical order: csharp, css3, html5, javascript. That ordering is what
	// settles which is which — they are indistinguishable from one another in a
	// diff, and css and html went in the wrong way round on the first pass.
	iconCSharp  = "\U000F031B" // 󰌛 nf-md-language_csharp
	iconCSS     = "\U000F031C" // 󰌜 nf-md-language_css3
	iconHTML    = "\U000F031D" // 󰌝 nf-md-language_html5
	iconJS      = "\U000F031E" // 󰌞 nf-md-language_javascript
	iconTS      = "\U000F0C79" // 󰱹 nf-md-language_typescript
	iconC       = "\U000F0671" // 󰙱 nf-md-language_c
	iconCPP     = "\U000F0672" // 󰙲 nf-md-language_cpp
	iconSwift   = "\U000F06E5" // 󰛥 nf-md-language_swift
	iconLua     = "\U000F08B1" // 󰢱 nf-md-language_lua
	iconSQL     = "\U000F01BC" // 󰆼 nf-md-database
	iconLicence = "\U000F0BC3" // 󰯃 nf-md-certificate_outline
	iconKey     = "\U000F0306" // 󰌆 nf-md-key
	iconArchive = "\U000F05C1" // 󰗁 nf-md-zip_box
	iconImage   = "\U000F021F" // 󰈟 nf-md-file_image
	iconVideo   = "\U000F0381" // 󰎁 nf-md-video
	iconAudio   = "\U000F0388" // 󰎈 nf-md-music_note
	iconPDF     = "\U000F0226" // 󰈦 nf-md-file_pdf_box
	iconText    = "\U000F0219" // 󰈙 nf-md-file_document
	iconEnv     = "\U000F0493" // 󰒓 nf-md-cog
	iconLock    = "\U000F033E" // 󰌾 nf-md-lock
)

// basenames are the files whose *whole name* is the declaration.
//
// Consulted before extensions, which is what makes `Dockerfile`, `Makefile` and
// `LICENSE` — none of which has an extension — resolvable at all, and what
// keeps `.gitlab-ci.yml` from being just another YAML.
var basenames = map[string]string{
	"dockerfile":          theme.IconDocker,
	"containerfile":       theme.IconDocker,
	"docker-compose.yml":  theme.IconDocker,
	"docker-compose.yaml": theme.IconDocker,
	"compose.yml":         theme.IconDocker,
	"compose.yaml":        theme.IconDocker,
	".dockerignore":       theme.IconDocker,

	"makefile":  theme.IconTools,
	"justfile":  theme.IconTools,
	"taskfile":  theme.IconTools,
	"mise.toml": theme.IconTools,

	".gitlab-ci.yml":  theme.IconGitlab,
	".gitlab-ci.yaml": theme.IconGitlab,
	".gitignore":      theme.IconGitBranch,
	".gitattributes":  theme.IconGitBranch,
	".gitmodules":     theme.IconGitBranch,
	".gitleaksignore": theme.IconSecurity,

	"go.mod":  theme.IconGo,
	"go.sum":  theme.IconGo,
	"go.work": theme.IconGo,

	"cargo.toml":        theme.IconRust,
	"cargo.lock":        theme.IconRust,
	"package.json":      theme.IconNode,
	"package-lock.json": theme.IconNode,
	"pnpm-lock.yaml":    theme.IconNode,
	"yarn.lock":         theme.IconNode,
	"gemfile":           theme.IconRuby,
	"gemfile.lock":      theme.IconRuby,
	"composer.json":     theme.IconPHP,
	"pom.xml":           theme.IconJava,
	"build.gradle":      theme.IconJava,
	"mix.exs":           theme.IconElixir,
	"pyproject.toml":    theme.IconPython,
	"requirements.txt":  theme.IconPython,
	"pipfile":           theme.IconPython,

	"license":       iconLicence,
	"licence":       iconLicence,
	"copying":       iconLicence,
	"readme":        iconMarkdown,
	"changelog":     iconMarkdown,
	".env":          iconEnv,
	".editorconfig": theme.IconConfig,
}

// extensions decide when the whole name did not.
var extensions = map[string]string{
	".go":   theme.IconGo,
	".rs":   theme.IconRust,
	".py":   theme.IconPython,
	".rb":   theme.IconRuby,
	".php":  theme.IconPHP,
	".java": theme.IconJava,
	".ex":   theme.IconElixir,
	".exs":  theme.IconElixir,

	".js":   iconJS,
	".mjs":  iconJS,
	".cjs":  iconJS,
	".jsx":  iconJS,
	".ts":   iconTS,
	".tsx":  iconTS,
	".html": iconHTML,
	".htm":  iconHTML,
	".css":  iconCSS,
	".scss": iconCSS,

	".c":     iconC,
	".h":     iconC,
	".cpp":   iconCPP,
	".cc":    iconCPP,
	".hpp":   iconCPP,
	".cs":    iconCSharp,
	".swift": iconSwift,
	".lua":   iconLua,

	".sh":   iconShell,
	".bash": iconShell,
	".zsh":  iconShell,
	".fish": iconShell,
	".ps1":  iconShell,

	".md":       iconMarkdown,
	".markdown": iconMarkdown,
	".toml":     theme.IconToml,
	".yaml":     theme.IconConfig,
	".yml":      theme.IconConfig,
	".json":     theme.IconConfig,
	".xml":      theme.IconConfig,
	".ini":      theme.IconConfig,
	".conf":     theme.IconConfig,
	".cfg":      theme.IconConfig,
	".env":      iconEnv,

	".sql": iconSQL,
	".db":  iconSQL,

	".txt": iconText,
	".rst": iconText,
	".pdf": iconPDF,

	".zip": iconArchive,
	".gz":  iconArchive,
	".tar": iconArchive,
	".tgz": iconArchive,
	".xz":  iconArchive,
	".bz2": iconArchive,
	".7z":  iconArchive,
	".rar": iconArchive,

	".png":  iconImage,
	".jpg":  iconImage,
	".jpeg": iconImage,
	".gif":  iconImage,
	".svg":  iconImage,
	".webp": iconImage,
	".ico":  iconImage,

	".mp4":  iconVideo,
	".mkv":  iconVideo,
	".mov":  iconVideo,
	".webm": iconVideo,
	".mp3":  iconAudio,
	".wav":  iconAudio,
	".flac": iconAudio,

	".pem": iconLock,
	".crt": iconLock,
	".cer": iconLock,
	".key": iconKey,

	// Source files with no glyph of their own. IconCodeFile beats IconFile
	// here: "this is code" is less than naming the language and more than
	// nothing, and inventing a codepoint to say more would be a tofu box.
	".kt":    theme.IconCodeFile,
	".kts":   theme.IconCodeFile,
	".scala": theme.IconCodeFile,
	".clj":   theme.IconCodeFile,
	".hs":    theme.IconCodeFile,
	".dart":  theme.IconCodeFile,
	".zig":   theme.IconCodeFile,
	".pl":    theme.IconCodeFile,
	".pm":    theme.IconCodeFile,
	".vim":   theme.IconCodeFile,
	".nix":   theme.IconCodeFile,
	".tf":    theme.IconCodeFile,
}

// dockerfilePrefix catches the `Dockerfile.dev` / `Dockerfile.prod` family. A
// prefix rule rather than more entries, because the suffix is arbitrary — the
// same reasoning as internal/viewer's.
const dockerfilePrefix = "dockerfile."

// readmePrefix catches `README.md` and `README.rst` together. Without it the
// extension would win, which is right for `.md` and wrong for `.rst` — and the
// point of a README glyph is that it does not depend on which one somebody
// chose.
const readmePrefix = "readme."

// For returns the glyph naming a file, or theme.IconFile when nothing declares
// one. It never returns empty: a missing glyph shifts every name it prefixes by
// one cell, so the column would stop being a column.
func For(name string) string {
	base := strings.ToLower(filepath.Base(name))

	if icon, ok := basenames[base]; ok {
		return icon
	}
	if strings.HasPrefix(base, dockerfilePrefix) {
		return theme.IconDocker
	}
	if strings.HasPrefix(base, readmePrefix) {
		return iconMarkdown
	}
	if icon, ok := extensions[filepath.Ext(base)]; ok {
		return icon
	}
	return theme.IconFile
}
