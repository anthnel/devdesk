package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The vocabulary only matters if it is checked against the code. These
// tests do that: they scan the views' sources and verify that what they
// bind is indeed what this package declares. A source scan is fragile, and
// that is accepted — it is the price of the check, and internal/command
// already has this kind of contract test
// (TestEveryViewSuppliesItsHeaderAndHelp).

// scannedTrees are the trees where key bindings live.
var scannedTrees = []string{
	filepath.Join("..", "..", "app"),
	filepath.Join("..", ".."),
}

// structuralKeys are the keys whose meaning never changes. Their presence
// in a switch is what lets one recognize a key switch without relying on
// the name of the tested variable: a switch on runtime.GOOS or on a
// severity contains none of them.
var structuralKeys = map[string]bool{
	"esc": true, "enter": true, "tab": true, "shift+tab": true,
	"up": true, "down": true, "left": true, "right": true,
	"pgup": true, "pgdown": true, "home": true, "end": true,
	" ": true, "/": true, "?": true,
}

// navigationKeys are the ones whose lettered alias would be navigation.
var navigationKeys = map[string]bool{
	"up": true, "down": true, "left": true, "right": true,
	"pgup": true, "pgdown": true, "home": true, "end": true,
}

// retiredAliases have no surviving meaning in the application: neither an
// action (they are lowercase) nor a declared toggle. Encountering them in
// a key switch can only be a vim leftover.
//
// `h`, `l` and `f` are not in it: they survive as local toggles (HIGH and
// LOW severity in security, protocol in netdiag, format in the viewer).
// It's the clause that gives them away, not the letter — see
// TestNoBareLetterIsNavigation.
//
// `g` left this list with §3.53, and the reason is the one that brought
// `H` back into `free` in §3.47, taken the other way round: a letter that
// regains a meaning leaves the list of those that no longer have one,
// otherwise the list would lie. What it means in the viewer is not what
// §3.26 took away from it — it opens a prompt, and the jump takes an
// **argument**, which `home` and `end` do not and never will cover. `j`
// and `k` remain retired: they are only `down` and `up` under another
// name.
var retiredAliases = map[string]bool{"j": true, "k": true}

func TestEveryActionIsOneUppercaseLetter(t *testing.T) {
	for key, meaning := range actions {
		if len([]rune(key)) != 1 || key < "A" || key > "Z" {
			t.Errorf("action %q (%s) is not a single uppercase letter; the whole "+
				"namespace rule rests on that shape", key, meaning)
		}
		if strings.TrimSpace(meaning) == "" {
			t.Errorf("action %q carries no meaning", key)
		}
	}
}

// TestFreeLettersAreActuallyFree keeps the list of available letters from
// lying. It exists so the next addition doesn't have to redo the survey,
// so a stale entry is worse than no list at all.
func TestFreeLettersAreActuallyFree(t *testing.T) {
	for _, key := range Free() {
		if IsAction(key) {
			t.Errorf("%q is advertised as free but %s already claims it", key, actions[key])
		}
	}
	if got := len(actions) + len(free); got != 26 {
		t.Errorf("actions + free = %d, want 26 — a letter is either used or free, "+
			"and one that is neither is a letter nobody knows about", got)
	}
}

// TestNoLocalToggleShadowsAnAction verifies that the two namespaces stay
// disjoint. They are disjoint by case, but a toggle declared in uppercase
// by mistake would silently merge them back together.
func TestNoLocalToggleShadowsAnAction(t *testing.T) {
	for _, surface := range localToggles {
		for _, key := range surface.Keys {
			if key != strings.ToLower(key) {
				t.Errorf("%s declares %q as a toggle, but it is not lowercase; "+
					"a toggle changes nothing and an action does", surface.Name, key)
			}
		}
	}
}

// TestNoViewBindsAnUndeclaredUppercaseKey is the central test: any uppercase
// letter bound in a view must come from the vocabulary. It's this test that
// turns "keys should be consistent" into something that fails the build.
func TestNoViewBindsAnUndeclaredUppercaseKey(t *testing.T) {
	for _, clause := range keySwitchClauses(t) {
		for _, key := range clause.keys {
			if len([]rune(key)) != 1 || key < "A" || key > "Z" {
				continue
			}
			if IsAction(key) {
				continue
			}
			// A modal is a mode: it claims every key before the view sees
			// it, so its Y/N cannot collide with any action.
			if IsModalKey(key) && clause.isModal {
				continue
			}
			t.Errorf("%s: binds %q, which no action declares.\n"+
				"An uppercase letter is an action and its meaning is global "+
				"(internal/ui/keymap). Either reuse the action that already means "+
				"this, or add it there — free letters: %v",
				clause.where, key, Free())
		}
	}
}

// TestNoBareLetterIsNavigation is the rule bought by removing the vim
// aliases, and the only thing it buys.
//
// Two forms violate it. An alias — a letter in the same clause as a
// navigation key, `case "up", "k"` — and an isolated leftover, `case "j"`.
// The first is what made the rule unverifiable; keeping j/k alone would
// leave an exception, and it is exceptions that produced the state §3.26
// found.
func TestNoBareLetterIsNavigation(t *testing.T) {
	for _, clause := range keySwitchClauses(t) {
		hasNavigation := false
		for _, key := range clause.keys {
			if navigationKeys[key] {
				hasNavigation = true
			}
		}

		for _, key := range clause.keys {
			if len([]rune(key)) != 1 || !isLetter(key) {
				continue
			}
			if retiredAliases[key] {
				t.Errorf("%s: binds %q, a vim alias with no surviving meaning "+
					"(home/end already cover g/G)", clause.where, key)
				continue
			}
			if hasNavigation {
				t.Errorf("%s: binds %q alongside navigation keys %v — no bare "+
					"letter is navigation", clause.where, key, clause.keys)
			}
		}
	}
}

// TestOnlyThreeCtrlCombinationsSurvive: the Ctrl budget is ~14 keys and
// each one drags a constraint (multiplexer prefix, flow control, tty
// character). §3.26 keeps only three, and any action that resettled
// behind Ctrl would retake a spot Shift gives for free.
func TestOnlyThreeCtrlCombinationsSurvive(t *testing.T) {
	survivors := map[string]string{
		"ctrl+c": "SIGINT — quitter",
		"ctrl+r": "rafraîchir, et rien d'autre",
		"ctrl+p": "la ligne de commande",
	}
	for _, clause := range keySwitchClauses(t) {
		for _, key := range clause.keys {
			if !strings.HasPrefix(key, "ctrl+") {
				continue
			}
			if _, ok := survivors[key]; ok {
				continue
			}
			if IsException(key) {
				continue
			}
			t.Errorf("%s: binds %q.\n"+
				"Only ctrl+c, ctrl+r and ctrl+p survive; an action belongs to the "+
				"uppercase vocabulary (internal/ui/keymap). Declared exceptions: %v",
				clause.where, key, exceptionKeys())
		}
	}
}

// TestEveryLowercaseBindingIsDeclared: a lowercase letter changes nothing,
// so its meaning can be local — but "local" must mean declared, otherwise
// it's just "not surveyed", which is the state we're coming from.
func TestEveryLowercaseBindingIsDeclared(t *testing.T) {
	// What holds everywhere: modal shortcuts, declared exceptions, and
	// `q` — which the router owns and no view binds.
	everywhere := map[string]bool{"q": true}
	for key := range modalKeys {
		everywhere[key] = true
	}
	for _, e := range exceptions {
		everywhere[e.Key] = true
	}

	for _, clause := range keySwitchClauses(t) {
		surface, hasSurface := SurfaceFor(clause.where)

		for _, key := range clause.keys {
			if len([]rune(key)) != 1 || key < "a" || key > "z" {
				continue
			}
			if everywhere[key] || (hasSurface && slices.Contains(surface.Keys, key)) {
				continue
			}
			where := "no surface"
			if hasSurface {
				where = surface.Name + " (" + strings.Join(surface.Keys, " ") + ")"
			}
			t.Errorf("%s: binds %q, which %s declares.\n"+
				"A lowercase letter is a filter or a display toggle and changes "+
				"nothing; if it acts, it is an action and belongs in uppercase. "+
				"Declare it on the surface or move it (internal/ui/keymap).",
				clause.where, key, where)
		}
	}
}

// TestTheCommandModeKeyIsReachable keeps the reason for the choice with
// the choice. alt+: was just as valid, until it turned out it didn't
// reach macOS; what was missing wasn't a test but the list of what needed
// to be ruled out.
func TestTheCommandModeKeyIsReachable(t *testing.T) {
	forbidden := map[string]string{
		"ctrl+:": "\":\" is 0x3A, outside the 0x40-0x5F range Ctrl encodes",
		"alt+:":  "Option is not Meta on macOS Terminal.app or iTerm2 by default",
		"ctrl+a": "screen's prefix",
		"ctrl+b": "tmux's prefix",
		"ctrl+c": "SIGINT",
		"ctrl+d": "EOF",
		"ctrl+s": "flow control (XOFF)",
		"ctrl+q": "flow control (XON)",
		"ctrl+i": "TAB",
		"ctrl+m": "Enter",
		"ctrl+j": "LF",
		"ctrl+h": "Backspace",
	}
	if why, bad := forbidden[CommandMode]; bad {
		t.Fatalf("CommandMode is %q, which cannot reach the application: %s", CommandMode, why)
	}
}

// --- source scan ---

type caseClause struct {
	where string
	keys  []string
	// isModal marks a clause belonging to a modal's switch, recognized by
	// the fact that it binds y and n in lowercase. A modal is a mode, not
	// a view: it takes over before the view sees anything.
	isModal bool
}

// keySwitchClauses returns every key-switch clause from the views.
//
// A switch is recognized as being about keys when one of its clauses
// cites a structural key or a ctrl+/alt+ combination. This is safer than
// relying on the name of the tested expression: msg.String() is not the
// only form, and a switch on runtime.GOOS or on a severity contains none
// of these strings.
func keySwitchClauses(t *testing.T) []caseClause {
	t.Helper()

	seen := map[string]bool{}
	var clauses []caseClause

	for _, root := range scannedTrees {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// This package declares the vocabulary; it necessarily cites it.
			if strings.Contains(filepath.ToSlash(path), "internal/ui/keymap/") {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				sw, ok := n.(*ast.SwitchStmt)
				if !ok || sw.Body == nil {
					return true
				}
				parsed := clausesOf(sw, fset, path)
				if !isKeySwitch(parsed) {
					return true
				}
				if isModalSwitch(parsed) {
					for i := range parsed {
						parsed[i].isModal = true
					}
				}
				clauses = append(clauses, parsed...)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(clauses) == 0 {
		t.Fatal("no key switch found — the scan is broken, not the code")
	}
	return clauses
}

func clausesOf(sw *ast.SwitchStmt, fset *token.FileSet, path string) []caseClause {
	var out []caseClause
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || len(cc.List) == 0 {
			continue
		}
		var keys []string
		for _, expr := range cc.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			keys = append(keys, value)
		}
		if len(keys) == 0 {
			continue
		}
		position := fset.Position(cc.Pos())
		out = append(out, caseClause{
			where: filepath.ToSlash(path) + ":" + strconv.Itoa(position.Line),
			keys:  keys,
		})
	}
	return out
}

func isKeySwitch(clauses []caseClause) bool {
	for _, clause := range clauses {
		for _, key := range clause.keys {
			if structuralKeys[key] ||
				strings.HasPrefix(key, "ctrl+") ||
				strings.HasPrefix(key, "alt+") ||
				strings.HasPrefix(key, "shift+") {
				return true
			}
		}
	}
	return false
}

// isModalSwitch recognizes a confirmation modal's switch by the fact that
// it binds y and n in lowercase — which no view does.
func isModalSwitch(clauses []caseClause) bool {
	var yes, no bool
	for _, clause := range clauses {
		for _, key := range clause.keys {
			switch key {
			case "y":
				yes = true
			case "n":
				no = true
			}
		}
	}
	return yes && no
}

func exceptionKeys() []string {
	out := make([]string, 0, len(exceptions))
	for _, e := range exceptions {
		out = append(out, e.Key+" ("+e.Surface+")")
	}
	return out
}

func isLetter(key string) bool {
	return (key >= "a" && key <= "z") || (key >= "A" && key <= "Z")
}
