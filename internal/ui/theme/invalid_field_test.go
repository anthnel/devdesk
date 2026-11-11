package theme

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var sgrRe = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// underlineMap returns, for each visible rune of s, whether an underline is
// active when it is drawn.
func underlineMap(s string) (runes []rune, underlined []bool) {
	on := false
	pos := 0
	emit := func(text string) {
		for _, r := range text {
			runes = append(runes, r)
			underlined = append(underlined, on)
		}
	}
	for _, m := range sgrRe.FindAllStringSubmatchIndex(s, -1) {
		emit(s[pos:m[0]])
		pos = m[1]
		params := s[m[2]:m[3]]
		if params == "" {
			on = false
			continue
		}
		for _, p := range strings.Split(params, ";") {
			switch p {
			case "0", "24", "4:0":
				on = false
			case "4", "4:1", "4:2", "4:3", "4:4", "4:5":
				on = true
			}
		}
	}
	emit(s[pos:])
	return runes, underlined
}

func TestInvalidFieldColorContrastsWithBackground(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		if ColorInvalidField == nil {
			t.Fatalf("theme %s: ColorInvalidField is not set by ApplyTheme", name)
		}
		ratio, err := ContrastRatio(ColorInvalidField, ColorBackground)
		if err != nil {
			t.Fatalf("theme %s: %v", name, err)
		}
		if ratio < 3.0 {
			t.Errorf("theme %s: ColorInvalidField on ColorBackground is %.2f:1, want >= 3:1", name, ratio)
		}
	})
}

func TestStyleInvalidFieldFallsBackToASingleUnderline(t *testing.T) {
	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set")
	}
	base := lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText)
	out := StyleInvalidField(base).Render("x")

	r, g, b, _ := ColorInvalidField.RGBA()
	wantUC := fmt.Sprintf("58;2;%d;%d;%d", r>>8, g>>8, b>>8)

	found := false
	for _, m := range sgrRe.FindAllStringSubmatch(out, -1) {
		params := strings.Split(m[1], ";")
		plain, curly := -1, -1
		for i, p := range params {
			switch p {
			case "4":
				if plain < 0 {
					plain = i
				}
			case "4:3":
				curly = i
			}
		}
		if plain < 0 || curly < 0 {
			continue
		}
		found = true
		if plain > curly {
			t.Errorf("single underline (4) must come before 4:3, got %q", m[1])
		}
		if !strings.Contains(m[1], wantUC) {
			t.Errorf("sequence %q lacks the underline colour %q (SGR 58)", m[1], wantUC)
		}
	}
	if !found {
		t.Errorf("no SGR sequence holds both 4 and 4:3 in %q", out)
	}
}

func TestStyleInvalidFieldAddsNoCell(t *testing.T) {
	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set")
	}
	base := lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText)
	for _, text := range []string{"80a0", "my app", ""} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			plain := base.Render(text)
			invalid := StyleInvalidField(base).Render(text)
			if got, want := lipgloss.Width(invalid), lipgloss.Width(plain); got != want {
				t.Errorf("width = %d, want %d", got, want)
			}
			if got, want := ansi.Strip(invalid), ansi.Strip(plain); got != want {
				t.Errorf("text = %q, want %q", got, want)
			}
		})
	}
	// A non-empty value must really be underlined, or the cases above prove nothing.
	if !strings.Contains(StyleInvalidField(base).Render("80a0"), "4:3") {
		t.Error("StyleInvalidField does not underline")
	}
}

func TestStyleInvalidFieldLeavesSpacesPlain(t *testing.T) {
	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set")
	}
	base := lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText)
	out := StyleInvalidField(base).Render("a b")
	runes, ul := underlineMap(out)
	if string(runes) != "a b" {
		t.Fatalf("visible text = %q, want %q", string(runes), "a b")
	}
	want := []bool{true, false, true}
	for i := range want {
		if ul[i] != want[i] {
			t.Errorf("rune %q underlined = %v, want %v", runes[i], ul[i], want[i])
		}
	}
	// The space keeps the theme background.
	br, bgc, bb, _ := ColorBackground.RGBA()
	wantBg := fmt.Sprintf("48;2;%d;%d;%d", br>>8, bgc>>8, bb>>8)
	idx := strings.Index(out, " ")
	start := strings.LastIndex(out[:idx], "\x1b[")
	if start < 0 || !strings.Contains(out[start:idx], wantBg) {
		t.Errorf("the space lost the background %q: %q", wantBg, out[max(start, 0):idx])
	}
}

func TestSetTextInputInvalidFalseIsByteIdentical(t *testing.T) {
	for _, focused := range []bool{true, false} {
		t.Run(strconv.FormatBool(focused), func(t *testing.T) {
			mk := func() textinput.Model {
				ti := textinput.New()
				ti.SetValue("80a0")
				ti.SetWidth(10)
				return ti
			}
			ref, got := mk(), mk()
			StyleTextInput(&ref)
			SetTextInputInvalid(&got, true)
			SetTextInputInvalid(&got, false)
			if focused {
				ref.Focus()
				got.Focus()
			}
			if r, g := ref.View(), got.View(); r != g {
				t.Errorf("View differs after true then false\nwant %q\ngot  %q", r, g)
			}
		})
	}
}

func TestAnInvalidTextInputUnderlinesTheValueNotThePadding(t *testing.T) {
	mk := func(invalid bool) textinput.Model {
		ti := textinput.New()
		ti.SetValue("80a0")
		ti.SetWidth(6)
		SetTextInputInvalid(&ti, invalid)
		return ti
	}
	valid, bad := mk(false), mk(true)
	vv, bv := valid.View(), bad.View()
	if !strings.Contains(bv, "4:3") {
		t.Fatalf("an invalid input does not underline its value: %q", bv)
	}
	if strings.Contains(vv, "4:3") {
		t.Errorf("a valid input is underlined: %q", vv)
	}
	if got, want := ansi.Strip(bv), ansi.Strip(vv); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := lipgloss.Width(bv), lipgloss.Width(vv); got != want {
		t.Errorf("width = %d, want %d", got, want)
	}
	runes, ul := underlineMap(bv)
	for i, r := range runes {
		isValue := strings.ContainsRune("80a0", r)
		if isValue && !ul[i] {
			t.Errorf("value rune %q is not underlined", r)
		}
		if r == ' ' && ul[i] {
			t.Errorf("padding cell %d is underlined", i)
		}
	}
}

func TestInvalidFieldColorIsAClosedAliasOfFooterError(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		if ColorInvalidField == nil {
			t.Fatalf("theme %s: ColorInvalidField is not set by ApplyTheme", name)
		}
		if ColorInvalidField != ColorFooterError {
			t.Errorf("theme %s: ColorInvalidField = %q, want ColorFooterError %q", name, Hex(ColorInvalidField), Hex(ColorFooterError))
		}
	})

	withTheme(t, separatedTheme())
	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set by ApplyTheme")
	}
	if ColorInvalidField == ColorError {
		t.Error("ColorInvalidField aliases ColorError; it must alias ColorFooterError")
	}
}

func TestInvalidFieldColorFollowsAThemeChange(t *testing.T) {
	th := DefaultTheme()
	th.Name = "invalid-field-changed"
	th.SeverityCritical = "#bb0022"
	withTheme(t, th)

	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set by ApplyTheme")
	}
	if got := Hex(ColorInvalidField); got != "#bb0022" {
		t.Errorf("Hex(ColorInvalidField) = %q, want %q", got, "#bb0022")
	}
}

func TestNoThemeKeyNamesTheInvalidFieldColor(t *testing.T) {
	typ := reflect.TypeOf(Theme{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if strings.Contains(strings.ToLower(tag), "invalid") {
			t.Errorf("Theme field %s has json key %q: the invalid-field colour is a closed alias", typ.Field(i).Name, tag)
		}
	}
}

func TestStyleInvalidFieldIsACurlyUnderline(t *testing.T) {
	if ColorInvalidField == nil {
		t.Fatal("ColorInvalidField is not set")
	}
	base := lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText)
	got := StyleInvalidField(base)
	if got.GetUnderlineStyle() != lipgloss.UnderlineCurly {
		t.Errorf("underline style = %v, want curly", got.GetUnderlineStyle())
	}
	if got.GetUnderlineColor() != ColorInvalidField {
		t.Errorf("underline colour = %v, want ColorInvalidField", got.GetUnderlineColor())
	}
	if got.GetUnderlineSpaces() {
		t.Error("spaces are underlined, want false")
	}
	if got.GetForeground() != base.GetForeground() || got.GetBackground() != base.GetBackground() {
		t.Error("foreground or background of base was not kept")
	}
}

func TestAnInvalidEmptyTextInputUnderlinesItsPlaceholder(t *testing.T) {
	mk := func(invalid bool) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = "name"
		ti.SetWidth(8)
		SetTextInputInvalid(&ti, invalid)
		return ti
	}
	valid, bad := mk(false), mk(true)
	vv, bv := valid.View(), bad.View()
	if !strings.Contains(bv, "4:3") {
		t.Fatalf("an invalid empty input does not underline its placeholder: %q", bv)
	}
	if strings.Contains(vv, "4:3") {
		t.Errorf("a valid empty input is underlined: %q", vv)
	}
	if got, want := ansi.Strip(bv), ansi.Strip(vv); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := lipgloss.Width(bv), lipgloss.Width(vv); got != want {
		t.Errorf("width = %d, want %d", got, want)
	}
}

func TestSetTextInputInvalidFalseEqualsStyleTextInput(t *testing.T) {
	mk := func() textinput.Model {
		ti := textinput.New()
		ti.Placeholder = "name"
		return ti
	}
	ref, got := mk(), mk()
	StyleTextInput(&ref)
	SetTextInputInvalid(&got, false)
	if !reflect.DeepEqual(ref.Styles(), got.Styles()) {
		t.Error("SetTextInputInvalid(ti, false) styles differ from StyleTextInput")
	}
	ref.SetWidth(8)
	got.SetWidth(8)
	if r, g := ref.View(), got.View(); r != g {
		t.Errorf("View differs\nwant %q\ngot  %q", r, g)
	}
}

func TestAnInvalidFocusedEmptyInputKeepsItsCursorAndUnderlinesTheRest(t *testing.T) {
	mk := func(invalid bool) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = "name"
		ti.SetWidth(8)
		SetTextInputInvalid(&ti, invalid)
		ti.Focus()
		return ti
	}
	valid, bad := mk(false), mk(true)
	vv, bv := valid.View(), bad.View()
	if got, want := lipgloss.Width(bv), lipgloss.Width(vv); got != want {
		t.Errorf("width = %d, want %d", got, want)
	}
	if got, want := ansi.Strip(bv), ansi.Strip(vv); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if strings.Contains(vv, "4:3") {
		t.Errorf("a valid focused input is underlined: %q", vv)
	}
	runes, ul := underlineMap(bv)
	if len(runes) == 0 {
		t.Fatalf("no visible runes in %q", bv)
	}
	// The first cell is the cursor: it takes the cursor style, not the underline.
	if ul[0] {
		t.Errorf("cursor cell %q is underlined, want the cursor style: %q", runes[0], bv)
	}
	if !strings.Contains(bv, "4:3") {
		t.Fatalf("the rest of the placeholder is not underlined: %q", bv)
	}
	// Past the cursor, the placeholder letters are underlined.
	for i, r := range runes {
		if i > 0 && r != ' ' && !ul[i] {
			t.Errorf("placeholder rune %q (cell %d) is not underlined", r, i)
		}
	}
}
