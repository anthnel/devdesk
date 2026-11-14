package netdiag

import (
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
)

const (
	noteNoContainer = "No running container — only this machine is offered"
	noteNoEngine    = "No Docker Engine reachable — only this machine is offered"
	noteDesktop     = "Docker Desktop is not supported as a container source yet"
	notePodman      = "podman is not supported as a container source"

	hostLabel          = "this machine"
	engineTextMaxRunes = 128
	dockerDesktopName  = "Docker Desktop"
)

// pathSource is where a path trace starts: this machine or a container.
type pathSource struct {
	ID    string // validated container ID; "" for this machine
	Name  string // display-safe
	Image string // display-safe
}

// hostSource returns the source that stands for this machine.
func hostSource() pathSource { return pathSource{} }

func (s pathSource) isHost() bool { return s.ID == "" }

// label is the text the selector and the table show for the source.
func (s pathSource) label() string {
	if s.isHost() {
		return hostLabel
	}
	return s.Name
}

// pathSourceList is what the selector offers besides this machine.
type pathSourceList struct {
	Containers []pathSource // running, sorted by Name; this machine is not in it
	Note       string       // why only this machine is offered; "" otherwise
}

// sourceListFrom builds the selector's list from what the engine answered.
// Precedence: podman, then any read error, then Docker Desktop, then the list.
func sourceListFrom(engineName, osName string, osErr error, cs []docker.Container, psErr error) pathSourceList {
	switch {
	case engineName == "podman":
		return pathSourceList{Containers: []pathSource{}, Note: notePodman}
	case osErr != nil || psErr != nil:
		return pathSourceList{Containers: []pathSource{}, Note: noteNoEngine}
	case isDockerDesktop(osName):
		return pathSourceList{Containers: []pathSource{}, Note: noteDesktop}
	}
	sources := []pathSource{}
	for _, c := range cs {
		if c.State != "running" || !docker.IsContainerID(c.ID) {
			continue
		}
		name := engineText(c.Name)
		if name == "" {
			continue
		}
		sources = append(sources, pathSource{ID: c.ID, Name: name, Image: engineText(c.Image)})
	}
	if len(sources) == 0 {
		return pathSourceList{Containers: sources, Note: noteNoContainer}
	}
	slices.SortStableFunc(sources, func(a, b pathSource) int { return strings.Compare(a.Name, b.Name) })
	return pathSourceList{Containers: sources}
}

// isDockerDesktop reports whether the engine's OS name is Docker Desktop's VM.
func isDockerDesktop(osName string) bool {
	return strings.HasPrefix(osName, dockerDesktopName)
}

// engineText makes a string an engine returned safe to display: valid UTF-8,
// no escape sequence, no control or format rune, whitespace runs collapsed to
// one space, at most engineTextMaxRunes runes.
func engineText(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			pendingSpace = b.Len() > 0
			continue
		case unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r):
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	out := []rune(b.String())
	if len(out) > engineTextMaxRunes {
		out = out[:engineTextMaxRunes]
	}
	return strings.TrimRight(string(out), " ")
}
