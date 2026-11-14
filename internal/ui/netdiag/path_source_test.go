package netdiag

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
)

const (
	idWeb = "aaaaaaaaaaaa"
	idDB  = "bbbbbbbbbbbb"
	idAPI = "cccccccccccc"
	idHos = "dddddddddddd"
)

func ctr(id, name, image, state string) docker.Container {
	return docker.Container{ID: id, Name: name, Image: image, State: state}
}

func sourceNames(l pathSourceList) []string {
	names := []string{}
	for _, s := range l.Containers {
		names = append(names, s.Name)
	}
	return names
}

func assertDisplaySafe(t *testing.T, what, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Errorf("%s %q is not valid UTF-8", what, s)
	}
	if ansi.Strip(s) != s || strings.ContainsRune(s, '\x1b') {
		t.Errorf("%s %q carries an escape sequence", what, s)
	}
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			t.Errorf("%s %q carries control/format rune %U", what, s, r)
		}
	}
	if n := utf8.RuneCountInString(s); n > 128 {
		t.Errorf("%s is %d runes, want <= 128", what, n)
	}
}

func TestPathSourceHost(t *testing.T) {
	h := hostSource()
	if !h.isHost() {
		t.Error("hostSource().isHost() = false")
	}
	if h.ID != "" {
		t.Errorf("host ID = %q, want empty", h.ID)
	}
	if got := h.label(); got != "this machine" {
		t.Errorf("host label = %q, want %q", got, "this machine")
	}
	c := pathSource{ID: idWeb, Name: "web-1"}
	if c.isHost() {
		t.Error("a container source reports isHost")
	}
	if got := c.label(); got != "web-1" {
		t.Errorf("container label = %q, want web-1", got)
	}
}

func TestPathSourceListFrom(t *testing.T) {
	engineErr := errors.New("cannot connect")
	okOS := "Debian GNU/Linux 13 (trixie)"
	running := []docker.Container{
		ctr(idWeb, "web-1", "nginx", "running"),
		ctr(idDB, "db", "postgres", "paused"),
		ctr(idAPI, "api", "app", "running"),
		ctr("xyz", "bad-id", "x", "running"),
	}
	tests := []struct {
		name       string
		engine, os string
		osErr      error
		cs         []docker.Container
		psErr      error
		wantNames  []string
		wantNote   string
	}{
		{"running only, sorted, paused and bad ID left out", "docker", okOS, nil, running, nil, []string{"api", "web-1"}, ""},
		{"no containers at all", "docker", okOS, nil, nil, nil, []string{}, noteNoContainer},
		{"none running", "docker", okOS, nil, []docker.Container{
			ctr(idDB, "db", "postgres", "exited"),
			ctr(idHos, "p", "x", "restarting"),
			ctr(idWeb, "c", "x", "created"),
			ctr(idAPI, "d", "x", "dead"),
		}, nil, []string{}, noteNoContainer},
		{"only a bad ID running", "docker", okOS, nil, []docker.Container{ctr("xyz", "a", "i", "running")}, nil, []string{}, noteNoContainer},
		{"os read failed", "docker", "", engineErr, running, nil, []string{}, noteNoEngine},
		{"ps failed", "docker", okOS, nil, nil, engineErr, []string{}, noteNoEngine},
		{"ps failed even with rows", "docker", okOS, nil, running, engineErr, []string{}, noteNoEngine},
		{"names empty after cleaning are left out", "docker", okOS, nil, []docker.Container{
			ctr(idWeb, "\x1b[0m", "x", "running"),
			ctr(idDB, "\xe2\x80\x8b", "x", "running"), // U+200B as raw bytes
		}, nil, []string{}, noteNoContainer},
		{"IDs invalid before cleaning are left out", "docker", okOS, nil, []docker.Container{
			ctr(idWeb+"\x1b[0m", "esc-id", "x", "running"),
			ctr(idDB+" ", "space-id", "x", "running"),
			ctr(idAPI, "ok", "x", "running"),
		}, nil, []string{"ok"}, ""},
		{"docker desktop and ps failed", "docker", "Docker Desktop", nil, running, engineErr, []string{}, noteNoEngine},
		{"docker desktop", "docker", "Docker Desktop", nil, running, nil, []string{}, noteDesktop},
		{"docker desktop with version", "docker", "Docker Desktop 4.96", nil, running, nil, []string{}, noteDesktop},
		{"podman", "podman", okOS, nil, running, nil, []string{}, notePodman},
		{"podman whatever the errors", "podman", "", engineErr, nil, engineErr, []string{}, notePodman},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceListFrom(tt.engine, tt.os, tt.osErr, tt.cs, tt.psErr)
			if names := sourceNames(got); !reflect.DeepEqual(names, tt.wantNames) {
				t.Errorf("containers = %v, want %v", names, tt.wantNames)
			}
			if got.Note != tt.wantNote {
				t.Errorf("note = %q, want %q", got.Note, tt.wantNote)
			}
			for _, s := range got.Containers {
				if s.isHost() || !docker.IsContainerID(s.ID) {
					t.Errorf("source %+v is not a container with a valid ID", s)
				}
			}
		})
	}
}

func TestPathSourceListFromCarriesIDAndImage(t *testing.T) {
	got := sourceListFrom("docker", "Debian", nil, []docker.Container{ctr(idWeb, "web-1", "nginx:1", "running")}, nil)
	want := []pathSource{{ID: idWeb, Name: "web-1", Image: "nginx:1"}}
	if !reflect.DeepEqual(got.Containers, want) {
		t.Errorf("containers = %+v, want %+v", got.Containers, want)
	}
}

func TestPathSourceListFromDoesNotMutateInput(t *testing.T) {
	in := []docker.Container{
		ctr(idWeb, "we\x1b[31mb", "im\nage", "running"),
		ctr(idAPI, "api", "app", "running"),
		ctr(idDB, "db", "pg", "paused"),
	}
	snapshot := slicesCloneContainers(in)
	sourceListFrom("docker", "Debian", nil, in, nil)
	if !reflect.DeepEqual(in, snapshot) {
		t.Errorf("input mutated:\n got %+v\nwant %+v", in, snapshot)
	}
}

func slicesCloneContainers(in []docker.Container) []docker.Container {
	out := make([]docker.Container, len(in))
	copy(out, in)
	return out
}

func TestPathSourceListFromHostileText(t *testing.T) {
	tests := []struct {
		name, rawName, rawImage string
		wantName                string
	}{
		{"escape sequence and RTL override", "we\x1b[31mb\u202e-1", "nginx\nlatest", "web-1"},
		{"OSC title sequence", "\x1b]0;pwn\aweb", "img", "web"},
		{"zero-width and bidi isolate", "w\u200be\u2066b", "i", "web"},
		{"invalid UTF-8", "web\xff\xfe-1", "i", "web-1"},
		{"control characters", "we\x00b\x07\x7f", "i", "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceListFrom("docker", "Debian", nil, []docker.Container{ctr(idWeb, tt.rawName, tt.rawImage, "running")}, nil)
			if len(got.Containers) != 1 {
				t.Fatalf("containers = %+v, want exactly one", got.Containers)
			}
			s := got.Containers[0]
			if s.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", s.Name, tt.wantName)
			}
			assertDisplaySafe(t, "Name", s.Name)
			assertDisplaySafe(t, "Image", s.Image)
			if strings.Contains(s.Name, "]0;") {
				t.Errorf("Name %q keeps an OSC payload", s.Name)
			}
		})
	}
}

func TestPathSourceListFromLongName(t *testing.T) {
	long := strings.Repeat("é", 300)
	got := sourceListFrom("docker", "Debian", nil, []docker.Container{ctr(idWeb, long, long, "running")}, nil)
	if len(got.Containers) != 1 {
		t.Fatalf("containers = %+v", got.Containers)
	}
	for what, s := range map[string]string{"Name": got.Containers[0].Name, "Image": got.Containers[0].Image} {
		if n := utf8.RuneCountInString(s); n != 128 {
			t.Errorf("%s = %d runes, want 128", what, n)
		}
	}
}

func TestEngineText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"valid text unchanged", "web-1", "web-1"},
		{"unicode text unchanged", "café-日本", "café-日本"},
		{"empty", "", ""},
		{"ANSI removed", "we\x1b[31mb-1", "web-1"},
		{"RTL override removed", "web\u202e-1", "web-1"},
		{"zero-width removed", "w\u200beb", "web"},
		{"control removed", "a\x00b\x07c", "abc"},
		{"newline collapsed", "nginx\nlatest", "nginx latest"},
		{"whitespace runs collapsed", "a \t  b", "a b"},
		{"invalid UTF-8 replaced away", "a\xffb", "ab"},
		{"capped at 128 runes", strings.Repeat("x", 300), strings.Repeat("x", 128)},
		{"capped by runes not bytes", strings.Repeat("日", 200), strings.Repeat("日", 128)},
		{"exactly 128 kept", strings.Repeat("y", 128), strings.Repeat("y", 128)},
		{"leading space dropped", " a", "a"},
		{"cut at a space leaves no trailing space", strings.Repeat("x", 127) + " y", strings.Repeat("x", 127)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engineText(tt.in)
			if got != tt.want {
				t.Errorf("engineText(%q) = %q, want %q", tt.in, got, tt.want)
			}
			assertDisplaySafe(t, "result", got)
		})
	}
}

func TestIsDockerDesktop(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"Docker Desktop", true},
		{"Docker Desktop 4.96", true},
		{"Ubuntu 24.04", false},
		{"Debian GNU/Linux 13 (trixie)", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isDockerDesktop(tt.in); got != tt.want {
			t.Errorf("isDockerDesktop(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestPathSourceNotesFitTheFooter(t *testing.T) {
	notes := map[string]string{
		"noteNoContainer": noteNoContainer, "noteNoEngine": noteNoEngine,
		"noteDesktop": noteDesktop, "notePodman": notePodman,
	}
	for name, n := range notes {
		if w := ansi.StringWidth(n); w == 0 || w > 60 {
			t.Errorf("%s width = %d, want 1..60", name, w)
		}
	}
}
