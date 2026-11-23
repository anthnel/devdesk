package scan

import (
	"os/exec"
	"strings"
	"unicode"
)

// shellMeta are the characters that make a shell read more than words in a
// command: separators, expansions, redirections, quoting, globbing, comments.
// The brackets are here for the glob they open (pkg[extra]).
const shellMeta = ";|&$`<>()\\'\"{}*?!#[]"

// wordHeadMeta are the characters a shell reads only at the head of a word: the
// home directory (~), the history substitution or glob negation (^), a job (%)
// and a command path (=, in zsh). Inside a word they are spellings real names
// and versions carry (semver@^6.3.1, 1:12.2.0~rc1-2, pkg==1.2,
// --flag=value), so only the head refuses them.
const wordHeadMeta = "~^%="

// PlainWords reports whether cmd is made of visible characters and single
// spaces, with no shell metacharacter. It is the one computation behind every
// command DevDesk offers to copy (DEVDESK-751, DEVDESK-752): the Run line of the
// finding detail, and a Repro line built here or read back from a cache.
func PlainWords(cmd string) bool {
	head := true
	for _, r := range cmd {
		switch {
		case r == ' ':
			head = true
			continue
		case unicode.IsControl(r), !unicode.IsPrint(r), strings.ContainsRune(shellMeta, r):
			return false
		case head && strings.ContainsRune(wordHeadMeta, r):
			return false
		}
		head = false
	}
	return true
}

// lookPath is the seam of the program name: whether the tool a scan ran is the
// one `name` resolves to on PATH. TestMain replaces it, so no test depends on
// the helm or the kustomize of the machine it runs on.
var lookPath = exec.LookPath

// reproLine joins a program, its arguments and the paths it works on into one
// line, or refuses.
//
// It refuses the whole line when a token is empty, holds a blank, or is not
// PlainWords: a path with a space gives no Repro rather than a command that
// splits it. A path that starts with "-" is refused too: once pasted, it would
// be read as an option. Nothing is quoted.
func reproLine(program string, args []string, paths ...string) (string, bool) {
	tokens := append(append([]string{program}, args...), paths...)
	for _, tok := range tokens {
		if tok == "" || strings.ContainsFunc(tok, unicode.IsSpace) || !PlainWords(tok) {
			return "", false
		}
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "-") {
			return "", false
		}
	}
	return strings.Join(tokens, " "), true
}

// reproProgram is the name to show for a program: the plain tool name when
// look finds the same binary (or when no binary is configured), else the
// configured path, which is what ran.
func reproProgram(binary, name string, look func(string) (string, error)) string {
	if binary == "" {
		return name
	}
	if found, err := look(name); err == nil && found == binary {
		return name
	}
	return binary
}

// localRepro is the Repro line of a finding one tool command could reproduce,
// and where the tool ran. A tool in a container gives no line and says so by its
// source; a line that cannot be written plainly gives none either. name is the
// tool's plain name and args the flags and subcommand DevDesk wrote, then the
// paths, run from the scanned directory. The user's own arguments
// (scan.tools.*.args) are never copied: they may hold a secret.
func localRepro(tool ToolSpec, name string, args []string, paths ...string) (repro string, from ToolSource) {
	switch tool.Source {
	case ToolSourceBinary:
		line, _ := reproLine(reproProgram(tool.Binary, name, lookPath), args, paths...)
		return line, ToolSourceBinary
	case ToolSourceContainer:
		return "", ToolSourceContainer
	}
	return "", ""
}
