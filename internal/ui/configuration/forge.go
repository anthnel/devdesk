package configuration

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
)

// The forge field, and the three things that make it more than a cycle.

// detectForgeFromURL adopts the platform a URL's host names, unless the user
// has already said which one it is.
//
// The dirty flag is the whole of it: without it, detection overwrites an
// explicit choice, which is the difference between helpful and possessive. A
// host it does not recognise changes nothing — self-hosted is the case that
// matters and `git.acme.com` could be either, so an unknown host is left to the
// user rather than guessed at confidently.
func (m Model) detectForgeFromURL() Model {
	if m.forgeTouched {
		return m
	}
	detected := forge.DetectType(m.config.Forge.URL)
	if detected == "" || detected == m.config.Forge.Type {
		return m
	}
	return m.adoptForge(detected)
}

// adoptForge writes a new platform and everything that follows from it.
//
// The visibility set is the part that cannot be left alone: GitHub.com has no
// `internal`, so a context carrying it would keep a value the server refuses
// and the cycle field would open on a value not in its own list. Coerced to the
// new shape's default, which is its most private.
//
// The field table is rebuilt because it is computed once from the type — the
// tab's title, its icon, the URL example, two labels and the visibility list
// all come from it. The router keeps this view on a save (that is what stops a
// save throwing away the cursor), so nothing else would rebuild it.
func (m Model) adoptForge(forgeType string) Model {
	m.config.Forge.Type = forgeType

	shape := forge.ShapeFor(forgeType)
	if !shape.AllowsVisibility(m.config.Forge.DefaultVisibility) {
		m.config.Forge.DefaultVisibility = shape.DefaultVisibility()
	}

	m.sections = sections(m.themes, command.ViewNames(), m.configPath, m.context, forgeType, forge.VocabularyFor(forgeType))
	return m
}

// commitForge settles the forge field when focus leaves it.
//
// On blur rather than on every ←→, for the reason the secret backend is: the
// session is closed by this change, and cycling through three values would
// close it three times — including on the way back to where it started.
//
// Nothing is asked. Changing the platform is not forbidden and not confirmed:
// the user is told what it did, which is the same call the URL makes one field
// below (§3.6).
func (m Model) commitForge() (Model, tea.Cmd) {
	if m.config.Forge.Type == m.forgeOnFocus {
		return m, nil
	}

	m = m.adoptForge(m.config.Forge.Type)
	m.forgeOnFocus = m.config.Forge.Type

	return m, tea.Batch(
		m.persist(saved{forgeChanged: true}),
		m.footer.Info("Forge is now "+m.vocab().Name+" — sign in again with :"+string(command.ViewGitAuth)),
	)
}
