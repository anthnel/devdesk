package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Every declared role resolves to something, and to something the palette
// actually holds. A role added to the enum but forgotten in the switch would
// return ColorText and be invisible in review — this is what makes it visible.
func TestEveryIconRoleHasAColour(t *testing.T) {
	roles := []IconRole{
		IconRoleNamespace, IconRoleRepository,
		IconRoleVisPublic, IconRoleVisInternal, IconRoleVisPrivate,
	}
	seen := map[IconRole]bool{}
	for _, role := range roles {
		got := IconColor(role)
		if got == "" {
			t.Errorf("IconColor(%q) is the zero Color", role)
		}
		if got == ColorText {
			t.Errorf("IconColor(%q) fell through to the default", role)
		}
		seen[role] = true
	}
	if len(seen) != len(roles) {
		t.Errorf("two roles share a name: %d distinct out of %d", len(seen), len(roles))
	}
}

// The two node kinds must not be the same colour: the whole point of the icon
// column in the explorer's selection mode is that the tick's *hue* still says
// group or repository once its shape has become a checkbox.
func TestANamespaceAndARepositoryAreToldApart(t *testing.T) {
	if IconColor(IconRoleNamespace) == IconColor(IconRoleRepository) {
		t.Error("a namespace and a repository share a colour — the selection mode loses the kind")
	}
}

// The three visibilities are three colours. Two that coincided would make the
// glyph the only difference, and the glyph is one cell wide.
func TestTheThreeVisibilitiesAreThreeColours(t *testing.T) {
	pub, internal, priv := IconColor(IconRoleVisPublic), IconColor(IconRoleVisInternal), IconColor(IconRoleVisPrivate)
	if pub == internal || pub == priv || internal == priv {
		t.Errorf("visibility colours collide: public=%v internal=%v private=%v", pub, internal, priv)
	}
}

// An unknown role is plain text, never the empty string — see IconColor.
func TestAnUnknownRoleReadsAsPlainText(t *testing.T) {
	if got := IconColor(IconRole("nothing-declares-this")); got != ColorText {
		t.Errorf("IconColor(unknown) = %v, want ColorText", got)
	}
}

// A theme file can name its own. This is the difference between these five and
// the syntax colours, which no theme may override.
func TestAThemeCanOverrideAnIconColour(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(DefaultTheme()) })

	custom := DefaultTheme()
	custom.IconVisPrivate = "#ff00ff"
	ApplyTheme(custom)

	if got := IconColor(IconRoleVisPrivate); string(got) != "#ff00ff" {
		t.Errorf("IconColor(private) = %v after a theme set it, want #ff00ff", got)
	}
	// The others keep their defaults: an override is one key, not the block.
	if IconColor(IconRoleVisPublic) != ColorOK {
		t.Error("overriding one icon colour moved another")
	}
}

// The style carries the colour and nothing else. A background here would fight
// the one datatable already paints (Rule 122).
func TestIconStyleCarriesOnlyTheForeground(t *testing.T) {
	style := IconStyle(IconRoleVisPublic)
	if got := style.GetForeground(); got != IconColor(IconRoleVisPublic) {
		t.Errorf("IconStyle foreground = %v, want %v", got, IconColor(IconRoleVisPublic))
	}
	if got := style.GetBackground(); got != (lipgloss.NoColor{}) {
		t.Errorf("IconStyle sets a background (%v) — datatable already paints one", got)
	}
}
