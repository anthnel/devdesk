package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"log"

	"charm.land/lipgloss/v2"
)

// Background is what DevDesk knows about the terminal's background.
type Background int

const (
	// BackgroundUnknown: no detection has run.
	BackgroundUnknown Background = iota
	BackgroundDark
	BackgroundLight
	// BackgroundNoAnswer: the terminal did not answer in time.
	BackgroundNoAnswer
)

// autoName is the value of app.theme that asks for the terminal-based choice.
const autoName = "auto"

// minContrastText is the WCAG AA floor for normal text.
const minContrastText = 4.5

// Resolution is the outcome of picking a theme for app.theme.
type Resolution struct {
	Requested string // value written in app.theme ("auto" or a name)
	Name      string // theme actually applied
	Theme     *Theme
	Reason    string // why this theme, "" for a named theme
	Warning   string // footer Warn text, "" when none
}

// ResolveNamed loads the theme called name, falling back to the default theme
// with a warning when it cannot be used.
func ResolveNamed(name string) Resolution {
	r := Resolution{Requested: name}
	th, warn := loadOrWarn(name, false)
	r.Theme, r.Warning = th, warn
	switch {
	case warn != "" || th.Name == "":
		r.Name = th.Name
	case name == "" || name == "dark":
		r.Name = "default"
	default:
		r.Name = name // the file name asked for, not the JSON "name" field
	}
	return r
}

// ResolveAuto picks lightName on a light terminal and darkName otherwise.
// A candidate that cannot be loaded or whose text is unreadable on its own
// background gives the default theme and a warning.
func ResolveAuto(bg Background, darkName, lightName string) Resolution {
	r := Resolution{Requested: autoName}
	name := darkName
	switch bg {
	case BackgroundLight:
		name, r.Reason = lightName, "terminal is light"
	case BackgroundDark:
		r.Reason = "terminal is dark"
	case BackgroundNoAnswer:
		r.Reason = "no answer from the terminal"
	}
	r.Theme, r.Warning = loadOrWarn(name, true)
	r.Name = name
	if r.Warning != "" {
		r.Name = r.Theme.Name
	} else if name == "" || name == "dark" {
		r.Name = "default"
	}
	return r
}

// loadOrWarn loads name; on any failure it returns the default theme and the
// footer text explaining it. withContrast adds the readability check.
func loadOrWarn(name string, withContrast bool) (*Theme, string) {
	th, err := LoadTheme(name)
	if err != nil {
		log.Printf("WARN [theme] %v", err)
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultTheme(), fmt.Sprintf("Theme %q not found — using default", name)
		}
		return DefaultTheme(), fmt.Sprintf("Theme %q is invalid — using default (see log)", name)
	}
	if withContrast {
		if err := MeetsMinimumContrast(th); err != nil {
			log.Printf("WARN [theme] theme %q: %v", name, err)
			return DefaultTheme(), fmt.Sprintf("Theme %q fails the contrast check — using default", name)
		}
	}
	return th, ""
}

// MeetsMinimumContrast checks that ColorText is readable on ColorBackground
// (at least 4.5:1, inclusive).
func MeetsMinimumContrast(t *Theme) error {
	ratio, err := ContrastRatio(lipgloss.Color(t.ColorText), lipgloss.Color(t.ColorBackground))
	if err != nil {
		return err
	}
	if ratio < minContrastText {
		return fmt.Errorf("text %s on background %s is %.2f:1, below %.1f:1", t.ColorText, t.ColorBackground, ratio, minContrastText)
	}
	return nil
}
