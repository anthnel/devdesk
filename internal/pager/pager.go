// Package pager resolves the program the log viewer pipes a document into.
//
// A pager is a command line a user writes (the PAGER variable, or app.pager),
// such as `less -R`. DevDesk cuts it into argv elements itself and starts the
// program directly: the line never reaches a shell, so `;`, `|`, `$(...)`, a
// backtick or a redirection is an ordinary character (OC-84, DEVDESK-801).
package pager

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnterminatedQuote is returned by Split for a line that opens a quote it
// never closes.
var ErrUnterminatedQuote = errors.New("unterminated quote")

// Split cuts a command line into argv elements.
//
// Elements are separated by blanks (space and tab). A single or double quote
// groups what it encloses, blanks included, and is dropped; the other kind of
// quote inside is an ordinary character. A backslash is an ordinary character
// too, so a Windows path needs no escaping. Nothing is expanded: no variable,
// no `~`, no glob. A blank line gives no element.
func Split(line string) ([]string, error) {
	var (
		argv    []string
		current strings.Builder
		inWord  bool
		quote   rune
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				argv = append(argv, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("%w in %q", ErrUnterminatedQuote, line)
	}
	if inWord {
		argv = append(argv, current.String())
	}
	return argv, nil
}

// Resolve picks the pager and returns its argv: the configured line (app.pager)
// if it is not blank, else env (the PAGER variable), else the platform default,
// `less -R`, or `more` on Windows.
//
// A pager that cannot be read is an error, never a silent fallback: replacing
// it would run a program the user did not pick.
//
// `less` shows the colours of a log only with -R, which the pager used to be
// given unconditionally. When the resolved program is `less` and no argument
// already asks for raw control characters, Resolve adds -R, so a bare
// PAGER=less keeps the colours.
func Resolve(configured, env, goos string) ([]string, error) {
	line := strings.TrimSpace(configured)
	if line == "" {
		line = strings.TrimSpace(env)
	}
	if line == "" {
		if goos == "windows" {
			return []string{"more"}, nil
		}
		return []string{"less", "-R"}, nil
	}
	argv, err := Split(line)
	if err != nil {
		return nil, err
	}
	if isLess(argv[0]) && !keepsColours(argv[1:]) {
		argv = append(argv, "-R")
	}
	return argv, nil
}

// isLess reports whether program names less: its base name, whichever kind of
// separator the path uses, with or without .exe.
func isLess(program string) bool {
	if i := strings.LastIndexAny(program, `/\`); i >= 0 {
		program = program[i+1:]
	}
	program = strings.ToLower(program)
	return program == "less" || program == "less.exe"
}

// keepsColours reports whether less is already told to pass colour sequences
// through: -R or -r (alone or inside a cluster such as -FRX), or the long forms.
func keepsColours(args []string) bool {
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			if strings.ToLower(a) == "--raw-control-chars" {
				return true
			}
		case strings.HasPrefix(a, "-"):
			if strings.ContainsAny(a, "Rr") {
				return true
			}
		}
	}
	return false
}
