package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// D63 — une touche annoncée qui n'agit pas.
//
// Rule 130 interdit une touche *grisée* qui agit quand même, et
// shortcut.Availability rend ça inexprimable : un seul champ, deux lecteurs.
// Rien ne gardait le sens inverse, et la vue security en portait deux instances
// dans le même état — `{Key: "o"}` annoncée pendant que le handler lisait
// `case keymap.Web:`, et `{Key: "esc/⌫"}` pendant qu'il ne lisait que `case
// "esc":`. Dans les deux cas la touche affichée ne faisait rien, et celle qui
// marchait n'était jamais montrée.
//
// Aucun test ne pouvait les voir, et la raison est structurelle : **la touche
// annoncée est une chaîne d'affichage, la touche liée est un nom de touche
// bubbletea**, et il n'existe aucune relation mécanique entre les deux. `↑↓` se
// lie par `case "up"`, `esc/⌫` par `case "esc"`. Un test qui rapprocherait
// naïvement les `{Key: …}` des `case …:` produirait surtout du bruit — un relevé
// grossier sort 52 candidats dont l'écrasante majorité sont des libellés d'aide
// (`Context`, `Images`, `Disk Usage`).
//
// Ce qui ferme la famille est donc de **créer** la relation là où elle peut
// exister : une touche du vocabulaire s'annonce par le même token que le
// handler teste. C'est ce que la correction de l'instance `o` avait déjà fait à
// la main avec `openPipelineKey`, et ces trois tests en font une contrainte de
// build plutôt qu'une bonne pratique.
//
// Ce qui n'est pas couvert, et délibérément : `↑↓`, `esc/⌫`, `enter/esc`,
// `tab / shift+tab`. Ce sont des chaînes d'affichage sans relation mécanique
// avec quoi que ce soit, et Rule 138 en tient déjà la plupart hors de l'écran.
// Deviner là produirait les 52 candidats.
//
// La granularité est le **paquet**, pas l'état de la vue. Une touche liée dans
// un onglet et annoncée dans un autre passe donc, et c'est le prix d'un scan de
// source : savoir dans quel état une clause s'applique demanderait de piloter la
// vue. Ce que ces tests attrapent est la touche liée *nulle part*, qui est ce
// qu'étaient les deux instances rapportées.

// TestNoAnnouncementSpellsAnActionOutInFull is the half that makes the other
// two possible: a Key written "S" is a string like any other, and nothing can
// tell it from a help label. Written keymap.Scan, it is the same token the
// handler tests.
func TestNoAnnouncementSpellsAnActionOutInFull(t *testing.T) {
	for _, a := range announcements(t) {
		if a.constant != "" || !IsAction(a.key) {
			continue
		}
		t.Errorf("%s: announces %q as a bare letter.\n"+
			"Write it as keymap.%s — an announcement and its binding have no "+
			"mechanical relation unless they are the same token, and that is "+
			"the whole of §1.3 D63.",
			a.where, a.key, actionName(a.key))
	}
}

// TestEveryAnnouncedActionIsBoundInItsPackage is the check itself: a view that
// shows `S Scan` must answer keymap.Scan somewhere in the same package.
//
// The package rather than the file, because a view's shortcuts and its handler
// are deliberately apart — availability.go, view.go, update.go.
func TestEveryAnnouncedActionIsBoundInItsPackage(t *testing.T) {
	bound := bindings(t)

	for _, a := range announcements(t) {
		if a.constant == "" || !IsAction(actionValue(a.constant)) {
			// keymap.CommandMode is announced by views the router answers for,
			// and it is the only constant here that is not an action.
			continue
		}
		if bound[a.pkg][a.constant] {
			continue
		}
		t.Errorf("%s: announces keymap.%s, which nothing in %s binds.\n"+
			"A key that is shown and does nothing is worse than one that is "+
			"absent: the user reaches for it, and the key that works is never "+
			"advertised. Bound here: %v",
			a.where, a.constant, a.pkg, sortedKeys(bound[a.pkg]))
	}
}

// TestEveryAnnouncedToggleIsBoundInItsPackage closes the family on the half the
// vocabulary cannot name.
//
// A lowercase toggle has no constant — its meaning is local by design — but it
// is still a single letter, so the announcement and the binding *can* be the
// same string and the comparison is exact rather than noisy. This is the test
// that would have caught the reported instance: security announced
// `{Key: "o"}` while its handler read `case keymap.Web:`.
func TestEveryAnnouncedToggleIsBoundInItsPackage(t *testing.T) {
	bound := bindings(t)

	for _, a := range announcements(t) {
		if len([]rune(a.key)) != 1 || a.key < "a" || a.key > "z" {
			continue
		}
		if bound[a.pkg][a.key] {
			continue
		}
		t.Errorf("%s: announces %q, which nothing in %s binds.\n"+
			"Either bind it, or advertise the key that actually does this — "+
			"a key shown and dead is what §1.3 D63 is. Bound here: %v",
			a.where, a.key, a.pkg, sortedKeys(bound[a.pkg]))
	}
}

// --- scan de source ---

// announcement is one `{Key: …, Description: …}` found in the sources.
//
// The pair of fields *is* the recogniser, rather than the type name: both
// shortcut.Shortcut and help.KeyBinding carry it, an element inside a slice
// literal has no type to read, and shortcut.HeaderInfo — the one other thing
// with a Key — has Value where these have Description.
type announcement struct {
	where string
	pkg   string
	// key is the key as a string, whether it was written as a literal or as a
	// package constant. constant is set instead when the Key is keymap.X, since
	// those are compared as constants rather than as text.
	key      string
	constant string
}

func announcements(t *testing.T) []announcement {
	t.Helper()

	consts := stringConstants(t)
	var out []announcement

	forEachSourceFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		pkg := packageDir(path)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			value, hasKey := fieldValue(lit, "Key")
			if _, hasDesc := fieldValue(lit, "Description"); !hasKey || !hasDesc {
				return true
			}

			a := announcement{
				where: filepath.ToSlash(path) + ":" + strconv.Itoa(fset.Position(lit.Pos()).Line),
				pkg:   pkg,
			}
			switch v := value.(type) {
			case *ast.BasicLit:
				a.key = unquoted(v)
			case *ast.Ident:
				// A shared constant is how the reported instance was fixed by
				// hand: one token, both sides. Resolving it here is what makes
				// that pattern checkable rather than merely tidy.
				a.key = consts[pkg][v.Name]
			case *ast.SelectorExpr:
				if p, ok := v.X.(*ast.Ident); ok && p.Name == "keymap" {
					a.constant = v.Sel.Name
				}
			}
			if a.key != "" || a.constant != "" {
				out = append(out, a)
			}
			return true
		})
	})

	if len(out) == 0 {
		t.Fatal("no shortcut or help entry found — the scan is broken, not the code")
	}
	return out
}

// bindings maps a package directory to everything its case clauses answer:
// keymap constant names and plain key strings alike, in one set. The two
// namespaces cannot collide — a constant name is never a key string.
func bindings(t *testing.T) map[string]map[string]bool {
	t.Helper()

	consts := stringConstants(t)
	bound := map[string]map[string]bool{}

	forEachSourceFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		pkg := packageDir(path)
		ast.Inspect(file, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range cc.List {
				var name string
				switch v := expr.(type) {
				case *ast.BasicLit:
					name = unquoted(v)
				case *ast.Ident:
					name = consts[pkg][v.Name]
				case *ast.SelectorExpr:
					if p, ok := v.X.(*ast.Ident); ok && p.Name == "keymap" {
						name = v.Sel.Name
					}
				}
				if name == "" {
					continue
				}
				if bound[pkg] == nil {
					bound[pkg] = map[string]bool{}
				}
				bound[pkg][name] = true
			}
			return true
		})
	})
	return bound
}

// stringConstants collects each package's own string constants, so a Key or a
// case written as a name resolves to the same text as one written inline.
func stringConstants(t *testing.T) map[string]map[string]string {
	t.Helper()

	out := map[string]map[string]string{}
	forEachSourceFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		pkg := packageDir(path)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if out[pkg] == nil {
						out[pkg] = map[string]string{}
					}
					out[pkg][name.Name] = unquoted(lit)
				}
			}
		}
	})
	return out
}

func forEachSourceFile(t *testing.T, visit func(string, *token.FileSet, *ast.File)) {
	t.Helper()

	seen := map[string]bool{}
	for _, root := range scannedTrees {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// This package declares the vocabulary; it names every constant by
			// definition.
			if strings.Contains(filepath.ToSlash(path), "internal/ui/keymap/") || seen[path] {
				return nil
			}
			seen[path] = true

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			visit(path, fset, file)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// fieldValue returns the value assigned to a named field in a composite
// literal, and whether the field is there at all.
func fieldValue(lit *ast.CompositeLit, name string) (ast.Expr, bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == name {
			return kv.Value, true
		}
	}
	return nil, false
}

func packageDir(path string) string { return filepath.ToSlash(filepath.Dir(path)) }

func unquoted(lit *ast.BasicLit) string {
	if lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

// actionConstants names each action, so an error message can say "write
// keymap.Scan" rather than leaving the reader to look it up.
var actionConstants = map[string]string{
	"New": New, "Edit": Edit, "Delete": Delete, "Rename": Rename,
	"Scan": Scan, "ScanAll": ScanAll, "Fetch": Fetch, "Clone": Clone,
	"Terminal": Terminal, "IDE": IDE, "Web": Web, "Logs": Logs,
	"Pager": Pager, "Kill": Kill, "Prune": Prune, "Browser": Browser,
	"Get": Get, "Auth": Auth, "Exclude": Exclude, "Requests": Requests,
	"Issues": Issues, "Copy": Copy,
}

func actionName(key string) string {
	for name, k := range actionConstants {
		if k == key {
			return name
		}
	}
	return "?"
}

func actionValue(name string) string { return actionConstants[name] }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
