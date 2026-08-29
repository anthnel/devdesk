package workspaces

import (
	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Ce que la ligne sélectionnée et la machine permettent, calculé une fois et lu
// par les deux moitiés de la vue : GetShortcuts pour griser, les handlers pour
// refuser.
//
// Le défaut évité est celui que scan.Categorize et Result.SecretVerdict ont eu
// chacun à défaire : deux règles pour une question, qui finissent par ne plus
// dire la même chose. Ici ce serait une touche grisée qui agit quand même — ou,
// pire, une touche offerte dont l'action retourne en silence.

// Les états viennent de `shortcut.Availability` : le header et le handler
// lisent le même champ, et il est partagé parce que toutes les vues en ont
// besoin (§3.48).
type actionState = shortcut.Availability

// available is the zero-reason state, spelled out where it reads better than an
// empty literal.
var available = actionState{}

// unavailable builds a refused state from the reason to show the user.
func unavailable(reason string) actionState { return shortcut.Unavailable(reason) }

// actionSet is every row-or-machine dependent action of the normal mode.
//
// Three of the view's keys are absent, and for one reason: they apply whatever
// the row is. T and O fall back to the browsed directory when nothing is
// selected, and **N creates a directory in the browsed directory too** — it
// never acts on the selection. Rule 130 used to hide N when a repository was
// selected, which said "this does not apply" about an action that worked
// perfectly; greying it would repeat that, and refusing it would break the
// invariant this file exists for. ctrl+r, /, ctrl+p and ? are likewise
// unconditional.
type actionSet struct {
	Enter  actionState
	Web    actionState
	Scan   actionState
	Sync   actionState
	All    actionState
	Rename actionState
	Delete actionState
	Copy   actionState
}

// The reasons, written once so the header, the footer and the tests cannot
// drift apart on the wording (Rule 129 — English US).
const (
	reasonNoRow        = "No entry selected"
	reasonNotARepo     = "Not a git repository"
	reasonNoScanTarget = "Not a git repository, and no repository nested under it"
	reasonNoRemote     = "This repository has no remote"
	reasonNothingToSee = "Nothing to open — scan this repository first"
	reasonNoScanner    = "No scanner available — install Trivy or Gitleaks, or check scan settings"
)

// reasonUnread is the one refusal that carries a number, and it is a function
// rather than a constant for that reason alone: how many places the walk could
// not look is the whole difference between "there are none" and "I could not
// tell". The other reasons are constants because they say the same thing every
// time (Rule 130).
func reasonUnread(skipped int) string {
	return "No repository found, and " + sharedcomponents.Plural(skipped, "directory", "directories") +
		" could not be read — check logs"
}

// actions works out what applies to the current row on the current machine.
func (m Model) actions() actionSet {
	entry, hasRow := m.selectedEntry()

	a := actionSet{
		Enter:  unavailable(reasonNoRow),
		Web:    unavailable(reasonNoRow),
		Scan:   unavailable(reasonNoRow),
		Sync:   unavailable(reasonNoRow),
		Rename: unavailable(reasonNoRow),
		Delete: unavailable(reasonNoRow),
		Copy:   unavailable(reasonNoRow),
		All:    m.scannerState(),
	}

	if !hasRow {
		// A scan still applies at the root: S falls back to the browsed
		// directory, which is what startSecurityScan does with an empty table.
		a.Scan = m.scannerState()
		return a
	}

	a.Rename = available
	a.Delete = available
	a.Copy = available

	switch {
	case !entry.IsDir:
		// A file opens in the viewer; the other three are a repository's, and
		// they each carry that reason rather than the "no row" one they were
		// initialised with — there *is* a row, it is simply not a repository.
		a.Enter = available
		a.Web = unavailable(reasonNotARepo)
		a.Scan = unavailable(reasonNotARepo)
		a.Sync = unavailable(reasonNotARepo)
	case entry.IsGitRepo:
		if _, scanned := m.scanCache[entry.Path]; scanned {
			a.Enter = available
		} else {
			a.Enter = unavailable(reasonNothingToSee)
		}
		// The browser opens the remote, so a repository without one has
		// nothing to open — IsGitRepo alone advertised W on a local-only clone
		// and openInBrowser then returned in silence.
		if entry.GitRemoteURL != "" {
			a.Web = available
		} else {
			a.Web = unavailable(reasonNoRemote)
		}
		a.Sync = available
		a.Scan = m.scannerState()
	default:
		a.Enter = unavailable(reasonNothingToSee)
		a.Web = unavailable(reasonNotARepo)
		// S and F share one targeting rule: a directory acts on the
		// repositories nested under it, at any depth.
		switch {
		case len(entry.SubRepoPaths) > 0:
			a.Sync = available
			a.Scan = m.scannerState()
		case entry.SubRepoSkipped > 0:
			// Not knowing is not knowing there are none. "No repository nested
			// under it" is a claim the walk is not entitled to make when it
			// could not read part of the tree, and saying it anyway is what
			// made D59 silent rather than merely annoying.
			why := reasonUnread(entry.SubRepoSkipped)
			a.Sync = unavailable(why)
			a.Scan = unavailable(why)
		default:
			a.Sync = unavailable(reasonNoScanTarget)
			a.Scan = unavailable(reasonNoScanTarget)
		}
	}

	return a
}

// guard refuses a key the header shows greyed out, and says why.
//
// The reason is the one actions() computed, so what the footer prints and what
// the header greys can never be about different things. A Warn rather than an
// Error: nothing failed, the action simply does not apply as asked (Rule 128).
func (m Model) guard(s actionState, act func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !s.Enabled() {
		return m, m.footer.Warn(s.Reason)
	}
	return act()
}

// scannerState says whether a scan can run at all on this machine.
//
// Not knowing is not knowing that not: while deps is nil the check has not come
// back yet, and greying S out for three frames to un-grey it afterwards reads as
// a fault. One scanner is enough — Trivy and Gitleaks answer different
// questions and either produces a result worth having.
func (m Model) scannerState() actionState {
	if m.deps == nil {
		return available
	}
	if m.deps.TrivyAvailable || m.deps.GitleaksAvailable {
		return available
	}
	return unavailable(reasonNoScanner)
}
