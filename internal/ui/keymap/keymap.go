// Package keymap declares the application's keyboard vocabulary.
//
// It does not read any key and does not depend on bubbletea: it is a list of
// strings, and its only role is to be the reference the tests check the code
// against (keymap_test.go). Without this reference, key consistency is a
// convention that nothing verifies — which is exactly the state §3.26 found:
// 184 bindings, 16 collisions.
//
// # Three disjoint namespaces
//
//   - An UPPERCASE letter is an action, and its meaning is global to the
//     application. `D` deletes, in every view, whatever "delete" means
//     wherever you are.
//   - A lowercase letter is a filter or a display toggle. It changes
//     nothing, so its meaning can be local and two views can use the same
//     letter without contradicting each other.
//   - Everything else (arrows, esc, enter, tab, space, `/`, `.`, `?`, `:`) is
//     structural and never changes.
//
// # Why Shift carries the actions
//
// This isn't a matter of taste, it's the real budget of a terminal:
//
//	Ctrl+letter  : only encodes ASCII 0x40-0x5F, and the tty confiscates
//	               four of them (ctrl+i = TAB, ctrl+m = Enter, ctrl+j = LF,
//	               ctrl+h = Backspace). ctrl+a and ctrl+b are screen's and
//	               tmux's prefixes, ctrl+c/ctrl+d are SIGINT and EOF,
//	               ctrl+s/ctrl+q are flow control.           → ~14 left
//	Alt+key      : Option is not Meta on macOS unless the user enables it;
//	               the application receives nothing.                → 0
//	Ctrl+Shift   : the control code erases case — ctrl+a and ctrl+shift+a
//	               both emit 0x01. Telling them apart requires the Kitty
//	               keyboard protocol or modifyOtherKeys, which bubbletea
//	               v1.3.10 does not enable (that's WithKeyboardEnhancements()
//	               in v2). And even then the emulator claims them first:
//	               ctrl+shift+c/v/t/w/n are copy, paste, tab, close, window.
//	                                                                  → 0
//	Shift+letter : no constraint. Works on every emulator, every platform,
//	               through SSH and tmux.                             → 26
//
// Shift+letter IS a two-key combination: two fingers, no accidental firing.
// It carries the vocabulary not by default, but because the two other
// families are crippled or unusable.
package keymap

import (
	"maps"
	"sort"
	"strings"
)

// The 23 actions. About forty actions used to exist for 26 letters: the
// rule only holds after merging synonyms (Kill = stop and kill,
// Delete = delete and remove, Terminal = terminal and shell) and because
// display toggles are excluded from the count.
const (
	New      = "N" // Create a resource from this context
	Edit     = "E" // Edit the selected resource
	Delete   = "D" // Delete the selected resource
	Rename   = "M" // Rename (mv)
	Scan     = "S" // Scan the selected target
	ScanAll  = "A" // Scan everything (modal: "purge the cache first" checkbox)
	Fetch    = "F" // Catch up with the source: fetch + fast-forward, or follow a stream
	Clone    = "C" // Enter clone selection
	Terminal = "T" // Open a terminal or shell
	IDE      = "O" // Open in the configured IDE
	Web      = "W" // Open a URL in the browser
	Logs     = "L" // Open the logs
	Pager    = "V" // Open in the system pager
	Kill     = "K" // Stop, kill (modal: Stop / Restart, or SIGKILL)
	Prune    = "P" // Remove unused resources
	Browser  = "B" // Open the multi-registry browser
	Get      = "G" // Pull (get) the image or tag
	Auth     = "U" // Login / logout of the registry — toggles based on the row's state
	Exclude  = "X" // Exclude — add to .gitleaksignore
	Requests = "R" // Open merge requests · PRs
	Issues   = "I" // Open issues
	// Copy is `Y` — yank. The letter was free even though a modal uses it
	// for "Yes": a modal claims every key before the view sees it, so the
	// two are never reachable at the same time.
	Copy = "Y" // Copy the selection's path to the clipboard
)

// CommandMode opens the command line, from anywhere — including from a
// focused text field.
//
// ctrl+p, and not ctrl+: : ":" is 0x3A, outside the 0x40-0x5F range Ctrl
// encodes, so "ctrl+:" never reaches the application. The fallback was
// alt+:, which has the opposite flaw: on Terminal.app and iTerm2,
// Option+Shift+; emits a literal character and the key doesn't arrive
// either. ctrl+p is free throughout the application, is not a tty control
// character, is not the prefix of any multiplexer, and its meaning is
// already learned — command palette.
const CommandMode = "ctrl+p"

// actions maps each action key to its meaning. This is the table the tests
// check the code against; the table in §3.26 is its readable form.
var actions = map[string]string{
	New:      "Create a resource from this context",
	Edit:     "Edit the selected resource",
	Delete:   "Delete the selected resource",
	Rename:   "Rename",
	Scan:     "Scan the selected target",
	ScanAll:  "Scan everything",
	Fetch:    "Catch up with the source",
	Clone:    "Enter clone selection",
	Terminal: "Open a terminal or shell",
	IDE:      "Open in the configured IDE",
	Web:      "Open a URL in the browser",
	Logs:     "Open the logs",
	Pager:    "Open in the system pager",
	Kill:     "Stop or kill",
	Prune:    "Remove unused resources",
	Browser:  "Open the multi-registry browser",
	Get:      "Pull the image or tag",
	Auth:     "Log in or out of the registry",
	Exclude:  "Exclude — add to .gitleaksignore",
	Requests: "Open merge requests · PRs",
	Issues:   "Open issues",
	Copy:     "Copy the selection's path to the clipboard",
}

// Actions returns a copy of the key → meaning table.
func Actions() map[string]string {
	out := make(map[string]string, len(actions))
	maps.Copy(out, actions)
	return out
}

// IsAction reports whether a key belongs to the action vocabulary.
func IsAction(key string) bool {
	_, ok := actions[key]
	return ok
}

// modalKeys are the shortcuts of a confirmation modal.
//
// This is a fourth namespace, and it is disjoint from the other three by
// mode rather than by case: a modal claims every key before the view sees
// it (priority 1 in every handleKeyMsg), so `N` cannot mean "create" there
// — the view underneath receives nothing. Same argument as for InEditMode.
//
// They are declared here because the §3.26 survey had not seen them, and
// an uppercase letter bound without a declaration is indistinguishable
// from drift.
var modalKeys = map[string]string{
	"y": "Yes", "Y": "Yes",
	"n": "No", "N": "No",
}

// ModalKeys returns the modal shortcuts.
func ModalKeys() map[string]string {
	out := make(map[string]string, len(modalKeys))
	maps.Copy(out, modalKeys)
	return out
}

// IsModalKey reports whether a key is a modal shortcut.
func IsModalKey(key string) bool {
	_, ok := modalKeys[key]
	return ok
}

// free lists the uppercase letters no action occupies. They are declared
// rather than left to be inferred: the next addition must know where to
// pick from without redoing the survey, and an action that settles
// somewhere other than here is a duplicate that goes unnoticed.
// H came back here with §3.47: it used to trace the route, and the trace
// was removed because it answered for the Docker VM rather than for the
// machine (D57). A letter an action has just freed up is re-declared
// free, otherwise it stays reserved for a use that no longer exists.
var free = []string{"H", "J", "Q", "Z"}

// Free returns the still-available uppercase letters, sorted.
func Free() []string {
	out := append([]string(nil), free...)
	sort.Strings(out)
	return out
}

// Surface is a screen, its files, and the lowercase letters it uses.
//
// Path is what ties a file to its surface, and it is there so the
// declaration is checkable: without it, "these letters are local to this
// view" is verified by merging every list, and `l` declared for netdiag
// would excuse `l` in the registries. Matching is done on the longest
// prefix, so netdiag/ports can narrow down netdiag.
type Surface struct {
	Name string
	Path string
	Keys []string
}

// localToggles inventories the lowercase letters used as display or filter
// toggles. They change nothing, so their meaning is local and the same
// letter can serve twice without contradiction — `l` is the protocol in
// netdiag and the LOW severity in security.
//
// A file that matches no surface is entitled to no lowercase letter at
// all: that is the default, and it is what makes the list worth keeping
// up to date.
var localToggles = []Surface{
	{"containers", "ui/containers/", []string{"r", "p", "s", "t", "z"}},
	{"viewer", "ui/viewer/", []string{"f", "c", "w", "v", "t", "n", "g", "s"}},
	{"netdiag/ports", "ui/netdiag/ports_model.go", []string{"t", "u", "l", "e", "n", "z"}},
	{"netdiag/checks", "ui/netdiag/", []string{"p"}},
	{"oci/browser", "ui/oci_resources/browser_", []string{"r"}},
	{"security/findings", "ui/security/", []string{"c", "h", "m", "l"}},
}

// LocalToggles returns the declared surfaces.
func LocalToggles() []Surface {
	return append([]Surface(nil), localToggles...)
}

// SurfaceFor ties a file path to its surface, by longest prefix. The
// second return says whether a surface was found.
func SurfaceFor(path string) (Surface, bool) {
	var best Surface
	found := false
	for _, s := range localToggles {
		if !strings.Contains(path, s.Path) {
			continue
		}
		if !found || len(s.Path) > len(best.Path) {
			best, found = s, true
		}
	}
	return best, found
}

// Exception is a key that deviates from the vocabulary, with its reason.
type Exception struct {
	Key     string
	Surface string
	Why     string
}

// exceptions are the two accepted deviations. They are written here
// because §3.26 requires it: an undeclared exception is indistinguishable
// from drift, and the next survey would "fix" it.
var exceptions = []Exception{
	{
		Key:     "o",
		Surface: "security/results/ci-tab",
		Why: "Ouvrir le pipeline résolu par le forge. Même raison que `c` : " +
			"l'action n'existe que sur un onglet, et brûler une des trois " +
			"dernières majuscules libres pour ça coûterait plus que ça ne " +
			"rapporte. `o` plutôt qu'une autre lettre parce qu'il veut déjà " +
			"dire « ouvrir ce que cet écran désigne » dans l'état détail de " +
			"la même vue, où il ouvre la référence d'un finding.",
	},
	{
		Key:     "ctrl+y",
		Surface: "oci/launch-form",
		Why: "Copier la commande docker run. Même raison, et ctrl+y ne heurte " +
			"aucun caractère de contrôle utilisé ailleurs.",
	},
}

// DeclaredExceptions returns the declared exceptions.
func DeclaredExceptions() []Exception {
	return append([]Exception(nil), exceptions...)
}

// IsException reports whether a key is a declared exception on a surface.
func IsException(key string) bool {
	for _, e := range exceptions {
		if e.Key == key {
			return true
		}
	}
	return false
}
