// Package contextcmd interprets the arguments of the :ctx command. It is pure:
// no I/O and no UI imports, so every decision is table-testable.
package contextcmd

import (
	"fmt"
	"strings"
)

// Snapshot is the state of the world Resolve decides against.
type Snapshot struct {
	Names             []string
	Current, Previous string
	Ready             bool
}

// Kind is what the caller must do with an Action.
type Kind int

const (
	// List opens the context list overlay.
	List Kind = iota
	// Switch loads the context named Name.
	Switch
	// Create creates the context named Name and switches to it.
	Create
	// Refuse does nothing but report Text.
	Refuse
	// KeepOpen leaves the command line open and reports Text.
	KeepOpen
)

// Level is the footer severity of an Action's Text.
type Level int

const (
	// Info is a neutral fact.
	Info Level = iota
	// Warn means the request cannot be honored as typed.
	Warn
	// Error means the request is invalid or failed.
	Error
)

// Action is the outcome of Resolve.
type Action struct {
	Kind  Kind
	Name  string
	Level Level
	Text  string
}

const (
	// WordNew and WordPrevious are the reserved words of the context command.
	WordNew      = "new"
	wordList     = "list"
	WordPrevious = "-"

	textUsageNew = "Usage: :ctx new <name>"
	textUsage    = "Usage: :ctx <name>"
	textNoPrev   = "No previous context in this session"
	textNotReady = "Contexts are still loading — try again"
)

// Footer texts the router needs outside Resolve, for a failure that surfaces
// after the decision (a race with the disk, a bad name reaching the loader).

// AlreadyText is the footer text of a switch to the current context.
func AlreadyText(name string) string { return textAlready(name) }

// InvalidText is the footer text of a name that fails the format.
func InvalidText(name string) string { return textInvalid(name) }

// ReservedText is the footer text of a reserved word used as a name.
func ReservedText(name string) string { return textReserved(name) }

// ExistsText is the footer text of a creation over an existing context.
func ExistsText(name string) string { return textExists(name) }

// NotFoundText is the footer text of a missing context, without suggestion.
func NotFoundText(name string) string { return textNotFound(name, nil) }

func textAlready(name string) string { return fmt.Sprintf("Already on context %q", name) }

func textInvalid(name string) string {
	return fmt.Sprintf("Invalid context name %q — use lowercase letters, digits and hyphens", name)
}

func textReserved(name string) string {
	return fmt.Sprintf("%q is reserved — choose another name", name)
}

func textExists(name string) string {
	return fmt.Sprintf("Context %q already exists — use :ctx %s to switch", name, name)
}

// maxListedMatches caps how many ambiguous matches the footer names.
const maxListedMatches = 10

func textAmbiguous(prefix string, matches []string) string {
	list := strings.Join(matches, ", ")
	if len(matches) > maxListedMatches {
		list = strings.Join(matches[:maxListedMatches], ", ") +
			fmt.Sprintf(", and %d more", len(matches)-maxListedMatches)
	}
	return fmt.Sprintf("%q matches %s — type more letters or press tab", prefix, list)
}

func textPreviousGone(name string) string {
	return fmt.Sprintf("Previous context %q no longer exists", name)
}

func textNotFound(name string, names []string) string {
	if near, ok := DidYouMean(name, names); ok {
		return fmt.Sprintf("Context %q not found — did you mean %q? Use :ctx new %s to create it", name, near, name)
	}
	return fmt.Sprintf("Context %q not found — use :ctx new %s to create it", name, name)
}

func refuse(level Level, text string) Action { return Action{Kind: Refuse, Level: level, Text: text} }

func keepOpen(level Level, text string) Action {
	return Action{Kind: KeepOpen, Level: level, Text: text}
}

// ValidName reports whether name is made of lowercase letters, digits and
// hyphens only, mirroring config.ValidateContextName without importing it.
// It is the single source of that rule on the UI side.
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// cleanNames drops empty names and duplicates, keeping the first occurrence.
func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != "" && !contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// switchTo switches to name, or reports that it is already the current one.
func switchTo(name string, s Snapshot) Action {
	if name == s.Current {
		return refuse(Info, textAlready(name))
	}
	return Action{Kind: Switch, Name: name}
}

// Resolve interprets the words after :ctx. highlighted is the suggestion the
// user chose with tab, or "" when none was chosen.
func Resolve(args []string, highlighted string, s Snapshot) Action {
	s.Names = cleanNames(s.Names)
	if len(args) == 0 || (len(args) == 1 && args[0] == wordList) {
		return Action{Kind: List}
	}
	if !s.Ready {
		return keepOpen(Warn, textNotReady)
	}
	if args[0] == WordNew {
		return resolveNew(args[1:], s)
	}
	if len(args) > 1 {
		return keepOpen(Warn, textUsage)
	}
	return resolveName(args[0], highlighted, s)
}

// resolveNew handles ":ctx new <name>".
func resolveNew(rest []string, s Snapshot) Action {
	if len(rest) != 1 {
		return keepOpen(Warn, textUsageNew)
	}
	name := rest[0]
	switch {
	case name == WordNew || name == wordList || name == WordPrevious:
		return refuse(Error, textReserved(name))
	case !ValidName(name):
		return refuse(Error, textInvalid(name))
	case contains(s.Names, name):
		return refuse(Warn, textExists(name))
	}
	return Action{Kind: Create, Name: name}
}

// resolveName handles ":ctx <arg>": previous, exact, prefix, or unknown.
func resolveName(arg, highlighted string, s Snapshot) Action {
	if arg == WordPrevious {
		if s.Previous == "" {
			return refuse(Warn, textNoPrev)
		}
		if !contains(s.Names, s.Previous) {
			return refuse(Error, textPreviousGone(s.Previous))
		}
		return switchTo(s.Previous, s)
	}
	if !ValidName(arg) {
		return refuse(Error, textInvalid(arg))
	}
	if contains(s.Names, arg) {
		return switchTo(arg, s)
	}
	matches := Match(arg, s.Names)
	switch {
	case len(matches) == 0:
		return refuse(Error, textNotFound(arg, s.Names))
	case len(matches) == 1:
		return switchTo(matches[0], s)
	case contains(matches, highlighted):
		return switchTo(highlighted, s)
	}
	return keepOpen(Warn, textAmbiguous(arg, matches))
}

// Match returns the names starting with prefix (case-insensitive), an exact
// match first, the others in their original order.
func Match(prefix string, names []string) []string {
	p := strings.ToLower(prefix)
	out := []string{}
	for _, n := range names {
		if strings.ToLower(n) == p {
			out = append(out, n)
		}
	}
	for _, n := range names {
		l := strings.ToLower(n)
		if l != p && strings.HasPrefix(l, p) {
			out = append(out, n)
		}
	}
	return out
}

// DidYouMean returns the closest name: edit distance <= min(2, half the
// shorter length), or a common start of at least 3 characters. Ties go to the
// first name; empty names are never offered.
func DidYouMean(name string, names []string) (string, bool) {
	best, bestDist := "", 0
	for _, n := range names {
		if n == "" {
			continue
		}
		d := editDistance(name, n)
		if d > maxDistance(name, n) && commonPrefixLen(name, n) < 3 {
			continue
		}
		if best == "" || d < bestDist {
			best, bestDist = n, d
		}
	}
	return best, best != ""
}

// maxDistance is the largest edit distance still considered a typo.
func maxDistance(a, b string) int {
	return min(2, min(len([]rune(a)), len([]rune(b)))/2)
}

func commonPrefixLen(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
