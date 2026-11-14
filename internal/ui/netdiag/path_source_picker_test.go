package netdiag

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func pickerSrc(name string) pathSource {
	return pathSource{ID: strings.Repeat("a", 12) + name, Name: name, Image: "img/" + name}
}

// pickerKey returns the string the picker's handler would read from a real key.
func pickerKey(name string) string { return testutil.Key(name).String() }

// pickerWith builds a loaded picker with n containers c00, c01, ... and the
// host as current source.
func pickerWith(n int) pathSourcePicker {
	cs := make([]pathSource, 0, n)
	for i := range n {
		cs = append(cs, pickerSrc(fmt.Sprintf("c%02d", i)))
	}
	return newPathSourcePicker(hostSource()).withList(pathSourceList{Containers: cs})
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestPathSourcePickerNewIsLoadingWithOnlyTheHost(t *testing.T) {
	p := newPathSourcePicker(hostSource())
	if len(p.sources) != 1 || !p.sources[0].isHost() {
		t.Fatalf("sources = %+v, want only this machine", p.sources)
	}
	if p.cursor != 0 || !p.loading {
		t.Errorf("cursor=%d loading=%v, want 0 and true", p.cursor, p.loading)
	}
	body := strings.Join(plain(p.render(80, 10)), "\n")
	if !strings.Contains(body, "this machine (current)") {
		t.Errorf("render lacks %q:\n%s", "this machine (current)", body)
	}
	// Rule 128: loading goes to the footer, never to the body.
	for _, bad := range []string{"Loading", "Reading", "loading"} {
		if strings.Contains(body, bad) {
			t.Errorf("body carries loading text %q:\n%s", bad, body)
		}
	}
}

func TestPathSourcePickerWithList(t *testing.T) {
	api, web := pickerSrc("api"), pickerSrc("web-1")
	tests := []struct {
		name       string
		current    pathSource
		list       pathSourceList
		wantNames  []string
		wantCursor int
		wantNote   string
	}{
		{"cursor on current container", web, pathSourceList{Containers: []pathSource{api, web}}, []string{"this machine", "api", "web-1"}, 2, ""},
		{"current vanished falls back to host", pickerSrc("gone"), pathSourceList{Containers: []pathSource{api, web}}, []string{"this machine", "api", "web-1"}, 0, ""},
		{"host current", hostSource(), pathSourceList{Containers: []pathSource{api}}, []string{"this machine", "api"}, 0, ""},
		{"only the host plus a note", hostSource(), pathSourceList{Containers: []pathSource{}, Note: noteNoContainer}, []string{"this machine"}, 0, noteNoContainer},
		{"note without containers keeps current host", web, pathSourceList{Note: noteNoEngine}, []string{"this machine"}, 0, noteNoEngine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := newPathSourcePicker(tt.current)
			got := before.withList(tt.list)
			if got.loading {
				t.Error("still loading after withList")
			}
			if !before.loading {
				t.Error("withList mutated its receiver")
			}
			var names []string
			for _, s := range got.sources {
				names = append(names, s.label())
			}
			if strings.Join(names, ",") != strings.Join(tt.wantNames, ",") {
				t.Errorf("rows = %v, want %v", names, tt.wantNames)
			}
			if got.cursor != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", got.cursor, tt.wantCursor)
			}
			if got.note != tt.wantNote {
				t.Errorf("note = %q, want %q", got.note, tt.wantNote)
			}
			if want := tt.wantNames[tt.wantCursor]; got.selected().label() != want {
				t.Errorf("selected = %q, want %q", got.selected().label(), want)
			}
		})
	}
}

func TestPathSourcePickerNoteStaysOutOfTheBody(t *testing.T) {
	p := newPathSourcePicker(hostSource()).withList(pathSourceList{Containers: []pathSource{}, Note: noteNoContainer})
	body := strings.Join(plain(p.render(80, 10)), "\n")
	if strings.Contains(body, "No running container") {
		t.Errorf("the note belongs to the footer (H4), not the body:\n%s", body)
	}
	if !strings.Contains(body, "this machine") {
		t.Errorf("host row missing:\n%s", body)
	}
}

func TestPathSourcePickerMove(t *testing.T) {
	const rows = 3
	tests := []struct {
		name       string
		n          int // containers; entries = n+1
		start      int
		keys       []string
		wantCursor int
	}{
		{"down", 10, 0, []string{"down"}, 1},
		{"down stays at the end", 2, 2, []string{"down"}, 2},
		{"up stays at 0", 10, 0, []string{"up"}, 0},
		{"up", 10, 3, []string{"up"}, 2},
		{"end", 10, 0, []string{"end"}, 10},
		{"home", 10, 10, []string{"home"}, 0},
		{"pgdown by rows", 10, 0, []string{"pgdown"}, rows},
		{"pgdown clamps", 10, 9, []string{"pgdown"}, 10},
		{"pgup by rows", 10, 8, []string{"pgup"}, 8 - rows},
		{"pgup clamps", 10, 1, []string{"pgup"}, 0},
		{"tab does nothing", 10, 4, []string{"tab"}, 4},
		{"shift+tab does nothing", 10, 4, []string{"shift+tab"}, 4},
		{"enter does nothing", 10, 4, []string{"enter"}, 4},
		{"esc does nothing", 10, 4, []string{"esc"}, 4},
		{"a letter does nothing", 10, 4, []string{"j", "k", "g", "G"}, 4},
		{"left and right do nothing", 10, 4, []string{"left", "right"}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pickerWith(tt.n)
			p.cursor = tt.start
			// start from a consistent window, as a previous move would leave it
			if tt.start >= rows {
				p.top = tt.start - rows + 1
			}
			for _, k := range tt.keys {
				p = p.move(pickerKey(k), rows)
				if p.cursor < p.top || p.cursor >= p.top+rows {
					t.Fatalf("after %q cursor %d outside window [%d,%d)", k, p.cursor, p.top, p.top+rows)
				}
			}
			if p.cursor != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", p.cursor, tt.wantCursor)
			}
		})
	}
}

func TestPathSourcePickerMoveReturnsANewValue(t *testing.T) {
	p := pickerWith(5)
	_ = p.move(pickerKey("down"), 3)
	if p.cursor != 0 {
		t.Errorf("move mutated its receiver: cursor = %d", p.cursor)
	}
}

func TestPathSourcePickerTopFollowsTheCursor(t *testing.T) {
	p := pickerWith(9) // 10 entries, 3 visible
	for i := range 9 {
		p = p.move(pickerKey("down"), 3)
		if p.cursor != i+1 {
			t.Fatalf("step %d: cursor = %d, want %d", i, p.cursor, i+1)
		}
		if p.cursor < p.top || p.cursor >= p.top+3 {
			t.Fatalf("step %d: cursor %d outside [%d,%d)", i, p.cursor, p.top, p.top+3)
		}
	}
	for i := range 9 {
		p = p.move(pickerKey("up"), 3)
		if p.cursor < p.top || p.cursor >= p.top+3 {
			t.Fatalf("up step %d: cursor %d outside [%d,%d)", i, p.cursor, p.top, p.top+3)
		}
	}
	if p.cursor != 0 || p.top != 0 {
		t.Errorf("back at the start: cursor=%d top=%d, want 0 0", p.cursor, p.top)
	}
}

func TestPathSourcePickerRenderShowsEveryEntryWhenTheTerminalGrows(t *testing.T) {
	p := pickerWith(9).move(pickerKey("end"), 3) // 10 entries, top = 7
	if p.top != 7 {
		t.Fatalf("fixture: top = %d, want 7", p.top)
	}
	body := strings.Join(plain(p.render(80, 12)), "\n")
	for i := range 9 {
		if name := fmt.Sprintf("c%02d", i); !strings.Contains(body, name) {
			t.Errorf("entry %s hidden although the box has room for all:\n%s", name, body)
		}
	}
	if !strings.Contains(body, "this machine") {
		t.Errorf("this machine hidden:\n%s", body)
	}
	if p.top != 7 {
		t.Errorf("render mutated its receiver: top = %d", p.top)
	}
}

func TestPathSourcePickerBoxWidth(t *testing.T) {
	// H2: clamp(width-4, 20, 60).
	tests := []struct{ width, want int }{{80, 60}, {200, 60}, {64, 60}, {60, 56}, {30, 26}, {24, 20}, {10, 20}}
	for _, tt := range tests {
		if got := pickerBoxWidth(tt.width); got != tt.want {
			t.Errorf("pickerBoxWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}

func TestPathSourcePickerRenderGeometry(t *testing.T) {
	long := pathSource{ID: strings.Repeat("b", 12) + "long", Name: strings.Repeat("n", 60), Image: strings.Repeat("i", 40)}
	// Full-width names: every cell is two columns wide, so counting runes
	// instead of cells overflows the box.
	cjk := pathSource{ID: strings.Repeat("e", 12) + "cjk", Name: strings.Repeat("日本語Ｗ", 12), Image: "画像/ＷＷＷ"}
	cjkList := pathSourceList{Containers: []pathSource{cjk, {ID: strings.Repeat("f", 12) + "w", Name: "日本語", Image: "x"}}}
	tests := []struct {
		name          string
		width, height int
		list          pathSourceList
		current       pathSource
	}{
		{"80x10", 80, 10, pathSourceList{Containers: []pathSource{pickerSrc("api"), pickerSrc("web-1")}}, pickerSrc("web-1")},
		{"60x5 long name", 60, 5, pathSourceList{Containers: []pathSource{long, pickerSrc("z")}}, long},
		{"24x3", 24, 3, pathSourceList{Containers: []pathSource{long, pickerSrc("z")}}, hostSource()},
		{"80x10 CJK", 80, 10, cjkList, cjk},
		{"60x6 CJK", 60, 6, cjkList, cjk},
		{"24x5 CJK", 24, 5, cjkList, cjk},
		{"80x10 note only host", 80, 10, pathSourceList{Containers: []pathSource{}, Note: noteNoContainer}, hostSource()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPathSourcePicker(tt.current).withList(tt.list)
			lines := p.render(tt.width, tt.height)
			if len(lines) == 0 {
				t.Fatal("render returned no line")
			}
			wantRows := min(len(p.sources), tt.height-2)
			if len(lines) != wantRows+2 {
				t.Errorf("%d lines, want %d rows + 2 borders", len(lines), wantRows)
			}
			box := pickerBoxWidth(tt.width)
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != box {
					t.Errorf("line %d width = %d, want %d: %q", i, w, box, ansi.Strip(l))
				}
				if strings.ContainsAny(ansi.Strip(l), "\x1b\a") {
					t.Errorf("line %d keeps a cut or foreign escape: %q", i, l)
				}
			}
			if !strings.Contains(ansi.Strip(lines[0]), "Trace from") {
				t.Errorf("title missing in the top border: %q", ansi.Strip(lines[0]))
			}
		})
	}
}

func TestPathSourcePickerRenderMarksTheCursorRow(t *testing.T) {
	p := newPathSourcePicker(pickerSrc("web-1")).withList(pathSourceList{Containers: []pathSource{pickerSrc("api"), pickerSrc("web-1")}})
	lines := plain(p.render(80, 10))
	if len(lines) != 5 {
		t.Fatalf("%d lines, want 3 rows + 2 borders", len(lines))
	}
	body := lines[1 : len(lines)-1]
	for i, l := range body {
		hasMark := strings.Contains(l, theme.IconCircleSmall+" ")
		if want := i == 2; hasMark != want {
			t.Errorf("row %d mark = %v, want %v: %q", i, hasMark, want, l)
		}
		if i != 2 && !strings.Contains(l, "  ") {
			t.Errorf("row %d lacks the two-space prefix: %q", i, l)
		}
	}
	if !strings.Contains(body[2], "web-1 (current)") {
		t.Errorf("current source not marked: %q", body[2])
	}
	if strings.Contains(body[1], "(current)") || strings.Contains(body[0], "(current)") {
		t.Errorf("(current) on the wrong row: %q / %q", body[0], body[1])
	}
	if !strings.Contains(body[1], "api") || !strings.Contains(body[1], "img/api") {
		t.Errorf("row lacks name and image: %q", body[1])
	}
}

func TestPathSourcePickerRenderTruncatesLongNames(t *testing.T) {
	long := pathSource{ID: strings.Repeat("c", 16), Name: strings.Repeat("n", 60), Image: "img"}
	p := newPathSourcePicker(hostSource()).withList(pathSourceList{Containers: []pathSource{long}})
	body := strings.Join(plain(p.render(60, 6)), "\n")
	if !strings.Contains(body, "…") {
		t.Errorf("no truncation mark for a 60-rune name in a %d-cell box:\n%s", pickerBoxWidth(60), body)
	}
	if strings.Contains(body, strings.Repeat("n", 60)) {
		t.Errorf("the long name was not cut:\n%s", body)
	}
}

func TestPathSourcePickerRenderNeverLeaksAnEngineEscape(t *testing.T) {
	list := sourceListFrom("docker", "Ubuntu", nil, []docker.Container{
		{ID: strings.Repeat("d", 64), Name: "x\x1b]0;pwn\ay", Image: "i\x1b[31mmg", State: "running"},
	}, nil)
	if len(list.Containers) != 1 {
		t.Fatalf("fixture: %d containers, want 1", len(list.Containers))
	}
	p := newPathSourcePicker(hostSource()).withList(list)
	lines := p.render(80, 10)
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 2 rows + 2 borders", len(lines))
	}
	for _, l := range lines {
		if s := ansi.Strip(l); strings.Contains(s, "]0;") || strings.ContainsAny(s, "\x1b\a") {
			t.Errorf("escape payload leaked: %q", s)
		}
		if strings.Contains(l, "\x1b]0;") {
			t.Errorf("raw OSC in the rendered line: %q", l)
		}
	}
}

func TestPathSourcePickerRenderIsPure(t *testing.T) {
	p := pickerWith(6).move(pickerKey("end"), 3)
	a := p.render(80, 6)
	b := p.render(80, 6)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Error("two renders of the same value differ")
	}
}
