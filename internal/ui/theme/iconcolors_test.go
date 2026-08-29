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
		IconRoleDirectory, IconRoleFile, IconRoleImage,
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

// The workspaces listing splits three ways, and the three must be three
// colours: the glyphs differ too, so the colour is reinforcement — but two
// classes sharing a hue would make the reinforcement say the wrong thing.
func TestTheThreeWorkspaceKindsAreThreeColours(t *testing.T) {
	repo, dir, file := IconColor(IconRoleRepository), IconColor(IconRoleDirectory), IconColor(IconRoleFile)
	if repo == dir || repo == file || dir == file {
		t.Errorf("workspace icon colours collide: repo=%v dir=%v file=%v", repo, dir, file)
	}
}

// The :sec inventory's first column has exactly two values, so they must not
// sit a notch apart — which is why an image takes the highlight rather than a
// third purple.
func TestAnImageAndARepositoryAreToldApart(t *testing.T) {
	if IconColor(IconRoleImage) == IconColor(IconRoleRepository) {
		t.Error("an image and a repository share a colour in the :sec inventory")
	}
}

// A repository is one colour across the three views that list one. This is the
// property a role exists for: the explorer's is remote and the other two are
// local, which is a difference of location and not of kind.
func TestARepositoryIsOneColourEverywhere(t *testing.T) {
	// One role, so this cannot fail by accident — it fails the day someone
	// splits it into a local and a remote role without meaning to.
	if IconColor(IconRoleRepository) != ColorIconRepository {
		t.Error("the repository role no longer resolves to the repository colour")
	}
}
