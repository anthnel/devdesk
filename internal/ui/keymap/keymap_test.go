package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Le vocabulaire n'a d'intérêt que s'il est opposé au code. Ces tests le font :
// ils parcourent les sources des vues et vérifient que ce qu'elles lient est
// bien ce que ce paquet déclare. Un scan de source est fragile, et c'est
// assumé — c'est le prix du contrôle, et internal/command a déjà ce type de
// test de contrat (TestEveryViewSuppliesItsHeaderAndHelp).

// scannedTrees sont les arbres où vivent les liaisons clavier.
var scannedTrees = []string{
	filepath.Join("..", "..", "app"),
	filepath.Join("..", ".."),
}

// structuralKeys sont les touches qui ne changent jamais de sens. Leur présence
// dans un switch est ce qui permet de reconnaître un switch de touches sans
// se fier au nom de la variable testée : un switch sur runtime.GOOS ou sur une
// sévérité n'en contient aucune.
var structuralKeys = map[string]bool{
	"esc": true, "enter": true, "tab": true, "shift+tab": true,
	"up": true, "down": true, "left": true, "right": true,
	"pgup": true, "pgdown": true, "home": true, "end": true,
	" ": true, "/": true, "?": true,
}

// navigationKeys sont celles dont un alias lettré serait de la navigation.
var navigationKeys = map[string]bool{
	"up": true, "down": true, "left": true, "right": true,
	"pgup": true, "pgdown": true, "home": true, "end": true,
}

// retiredAliases n'ont plus aucun sens survivant dans l'application : ni action
// (ce sont des minuscules), ni bascule déclarée. Les rencontrer dans un switch
// de touches ne peut être qu'un reliquat vim.
//
// `h`, `l` et `f` n'y sont pas : ils survivent comme bascules locales (sévérité
// HIGH et LOW dans security, protocole dans netdiag, format dans le viewer).
// C'est la clause qui les trahit, pas la lettre — voir TestNoBareLetterIsNavigation.
var retiredAliases = map[string]bool{"j": true, "k": true, "g": true}

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

// TestFreeLettersAreActuallyFree empêche la liste des disponibles de mentir.
// Elle existe pour que le prochain ajout n'ait pas à refaire le relevé, donc
// une entrée périmée est pire que pas de liste du tout.
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

// TestNoLocalToggleShadowsAnAction vérifie que les deux espaces de noms restent
// disjoints. Ils le sont par la casse, mais une bascule déclarée en majuscule
// par inadvertance les recollerait sans bruit.
func TestNoLocalToggleShadowsAnAction(t *testing.T) {
	for surface, keys := range localToggles {
		for _, key := range keys {
			if key != strings.ToLower(key) {
				t.Errorf("%s declares %q as a toggle, but it is not lowercase; "+
					"a toggle changes nothing and an action does", surface, key)
			}
		}
	}
}

// TestNoViewBindsAnUndeclaredUppercaseKey est le test central : toute majuscule
// liée dans une vue doit venir du vocabulaire. C'est lui qui transforme « les
// touches devraient être cohérentes » en quelque chose qui échoue au build.
func TestNoViewBindsAnUndeclaredUppercaseKey(t *testing.T) {
	for _, clause := range keySwitchClauses(t) {
		for _, key := range clause.keys {
			if len([]rune(key)) != 1 || key < "A" || key > "Z" {
				continue
			}
			if IsAction(key) {
				continue
			}
			// Une modale est un mode : elle réclame toute touche avant que la
			// vue ne la voie, donc son Y/N ne peut heurter aucune action.
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

// TestNoBareLetterIsNavigation est la règle achetée par la suppression des
// alias vim, et la seule chose qu'elle achète.
//
// Deux formes la violent. Un alias — une lettre dans la même clause qu'une
// touche de navigation, `case "up", "k"` — et un reliquat isolé, `case "j"`.
// La première est ce qui rendait la règle invérifiable ; garder j/k seuls
// laisserait une exception, et ce sont les exceptions qui ont produit l'état
// que §3.26 a relevé.
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

// TestTheCommandModeKeyIsReachable garde la raison du choix avec le choix.
// alt+: était juste aussi, jusqu'à ce qu'on découvre qu'elle n'atteignait pas
// macOS ; ce qui manquait n'était pas un test mais la liste de ce qu'il fallait
// écarter.
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

// --- scan de source ---

type caseClause struct {
	where string
	keys  []string
	// isModal marque une clause appartenant à un switch de modale, reconnu à ce
	// qu'il lie y et n en minuscules. Une modale est un mode, pas une vue :
	// elle prend la main avant que la vue ne voie quoi que ce soit.
	isModal bool
}

// keySwitchClauses rend toutes les clauses de switch de touches des vues.
//
// Un switch est reconnu comme portant sur des touches quand l'une de ses
// clauses cite une touche structurelle ou une combinaison ctrl+/alt+. C'est
// plus sûr que de se fier au nom de l'expression testée : msg.String() n'est
// pas la seule forme, et un switch sur runtime.GOOS ou sur une sévérité ne
// contient aucune de ces chaînes.
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
			// Ce paquet-ci déclare le vocabulaire ; il le cite forcément.
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

// isModalSwitch reconnaît le switch d'une modale de confirmation à ce qu'il lie
// y et n en minuscules — ce qu'aucune vue ne fait.
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

func isLetter(key string) bool {
	return (key >= "a" && key <= "z") || (key >= "A" && key <= "Z")
}
