package viewer

import (
	"path/filepath"
	"strings"
)

// detection is what the name and the content managed to say about a document.
//
// Declared separates "the name said JSON" from "the content started with a
// brace". Only the first is worth reporting a parse failure for: a Markdown
// file that happens to open with a tag is not a broken XML document, and
// telling the user it is would be noise about a file that is displaying fine.
type detection struct {
	Kind     Kind
	Declared bool
}

// extensionKinds are the extensions that decide, on their own, what a file is.
//
// A log, a YAML, a TOML, a Markdown, a Dockerfile and a shell script are only
// ever recognised here, by basename below, or declared by their producer — never
// sniffed from the content. "This looks like a log" is not a decidable question,
// and neither is "this looks like YAML": the registry `provider` field is the
// precedent, declared and never inferred from the URL, so nothing depends on
// what happened to be seen first.
var extensionKinds = map[string]Kind{
	".json":     KindJSON,
	".xml":      KindXML,
	".log":      KindLog,
	".yaml":     KindYAML,
	".yml":      KindYAML,
	".toml":     KindTOML,
	".md":       KindMarkdown,
	".markdown": KindMarkdown,
	".mkd":      KindMarkdown,
	".sh":       KindShell,
	".bash":     KindShell,
	".zsh":      KindShell,
	".ksh":      KindShell,

	// A file named `web.dockerfile` — the form a repository holding several of
	// them uses, since only one file per directory can be called Dockerfile.
	".dockerfile": KindDockerfile,

	// Go's filepath.Ext returns the whole name for a dotfile: Ext(".bashrc") is
	// ".bashrc". Listing them here rather than as basenames is not a shortcut,
	// it is where the lookup will actually find them.
	".bashrc":       KindShell,
	".zshrc":        KindShell,
	".profile":      KindShell,
	".bash_profile": KindShell,
	".zprofile":     KindShell,
}

// basenameKinds are the files whose *whole name* is the declaration, because
// they carry no extension to declare anything with.
//
// It has to be consulted before the extension, not after: `Dockerfile.dev` has
// the extension ".dev", which is in no table, so an extension-first lookup would
// settle it as plain text before the basename ever got a say.
var basenameKinds = map[string]Kind{
	"dockerfile":    KindDockerfile,
	"containerfile": KindDockerfile,
}

// dockerfilePrefix catches the `Dockerfile.dev` / `Dockerfile.prod` family. It
// is a prefix rule rather than more entries because the suffix is arbitrary —
// it is whatever the project calls that build.
const dockerfilePrefix = "dockerfile."

// DetectKind is what a document is, judged by its name and then its content.
func DetectKind(name string, data []byte) Kind {
	return detect(name, data).Kind
}

func detect(name string, data []byte) detection {
	base := strings.ToLower(filepath.Base(name))
	if kind, ok := basenameKinds[base]; ok {
		return detection{Kind: kind, Declared: true}
	}
	if strings.HasPrefix(base, dockerfilePrefix) {
		return detection{Kind: KindDockerfile, Declared: true}
	}

	ext := strings.ToLower(filepath.Ext(name))
	if kind, ok := extensionKinds[ext]; ok {
		return detection{Kind: kind, Declared: true}
	}
	// Any other extension is taken at its word: a .go or a .txt is text, and
	// sniffing it would open a Go file as a tree the first time someone started
	// one with a brace.
	if ext != "" {
		return detection{Kind: KindPlain}
	}
	return detection{Kind: sniff(data)}
}

// leadingNoise is what may sit in front of a document's first real character:
// whitespace, and a byte-order mark an editor left behind. Without the mark in
// this set, a BOM would hide the very byte this function exists to read.
const leadingNoise = " \t\r\n\ufeff"

// sniff guesses at a file with no extension at all — a Makefile, a hook, a dump
// someone redirected into a name. It looks at the first meaningful byte and
// nothing else; a document that then fails to parse simply falls back to text,
// with no complaint, because nothing claimed it was structured.
//
// Only JSON and XML are guessed at, and only because a leading brace or tag is
// the whole of the question. YAML and TOML are never sniffed: a file opening
// with `---` is not thereby YAML, and `[section]` is a line of prose in half the
// files that contain one.
//
// A shebang is the exception, and it is one worth writing down rather than
// leaving to be rediscovered as drift. `#!/usr/bin/env bash` is not an
// indication of what the file resembles: it is the file naming the interpreter
// it is to be run by, which is the most explicit declaration a script can carry
// — the same standing as an extension, minus the extension. Contrast `---`,
// which is Markdown front matter as often as it is a YAML stream.
func sniff(data []byte) Kind {
	head := data
	if len(head) > sniffSize {
		head = head[:sniffSize]
	}
	trimmed := strings.TrimLeft(string(head), leadingNoise)
	switch {
	case strings.HasPrefix(trimmed, "{"), strings.HasPrefix(trimmed, "["):
		return KindJSON
	case strings.HasPrefix(trimmed, "<"):
		return KindXML
	case shellShebang(trimmed):
		return KindShell
	default:
		return KindPlain
	}
}

// shellInterpreters are the shebang interpreters this viewer colours as shell.
//
// The list is short on purpose: a `#!/usr/bin/env python` is a shebang too, and
// answering it with the bash lexer would colour a Python file wrong — which is
// worse than leaving it plain, because it looks deliberate.
var shellInterpreters = []string{"sh", "bash", "zsh", "ksh", "dash", "ash"}

// shellShebang reports whether a document opens with a shell interpreter line.
func shellShebang(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "#!") {
		return false
	}
	line := trimmed
	if end := strings.IndexAny(line, "\r\n"); end >= 0 {
		line = line[:end]
	}
	// The interpreter is the last path segment of the line: "#!/bin/bash" and
	// "#!/usr/bin/env bash -e" both end up at "bash" once the arguments and the
	// directories are taken off.
	for _, field := range strings.Fields(line) {
		name := field
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		for _, interpreter := range shellInterpreters {
			if name == interpreter {
				return true
			}
		}
	}
	return false
}
