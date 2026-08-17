package theme_test

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

// scannedTrees sont les arbres où un rendu prématuré pourrait se glisser :
// tout `internal/`, plus le `main` du dépôt.
var scannedTrees = []string{
	filepath.Join("..", "..", ".."),
}

// TestNoPackageLevelVarRendersAString interdit qu'une variable de paquet soit
// initialisée par un rendu.
//
// Deux choses cassent quand ça arrive, et la seconde a coûté cher :
//
//  1. L'initialisation d'un paquet précède `ApplyTheme`, donc la chaîne est
//     figée sur les couleurs du thème par défaut et ne suit plus aucun
//     changement de thème. C'est le piège déjà documenté sur `ColorChartBg`
//     dans `ApplyTheme`.
//
//  2. Surtout : le premier rendu déclenche le `sync.Once` par lequel lipgloss
//     mémorise **définitivement** le profil de couleur du terminal
//     (`Renderer.ColorProfile`). Le profil se trouvait donc calculé pendant
//     l'init des paquets, c'est-à-dire avant la ligne de `main()` qui pose
//     `COLORTERM=truecolor` là où WSL ne l'a pas propagé. Tout le TUI
//     retombait en ANSI256, où le fond de chaque thème est quantifié sur la
//     palette 256 : `#1e1e2e` (default, mocha) devient le noir 232 et
//     `#24273a` (macchiato) le bleu marine 17. Le fond ne « respectait pas le
//     thème » parce qu'il n'en recevait jamais la couleur exacte.
//
// La correction est toujours la même et elle est gratuite : une fonction
// plutôt qu'une `var`, pour que le premier rendu ait lieu dans `View()`.
func TestNoPackageLevelVarRendersAString(t *testing.T) {
	for _, decl := range packageLevelVarInits(t) {
		ast.Inspect(decl.value, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, isRender := renderingCall(call); isRender {
				t.Errorf("%s: la variable de paquet %q est initialisée par %s. "+
					"Un rendu à l'init fige la chaîne sur le thème par défaut et "+
					"verrouille le profil de couleur de lipgloss avant que main() "+
					"n'ait posé COLORTERM — remplacer la var par une fonction.",
					decl.where, decl.name, name)
			}
			return true
		})
	}
}

// renderingCall reconnaît un appel qui produit une chaîne stylée, donc qui
// résout une couleur. Deux formes suffisent à les couvrir toutes :
// `<style>.Render(...)`, et n'importe quelle fonction du paquet theme — ses
// helpers (`Bg`, `BgLine`, `PadWithBg`, `EmptyLineBg`, `RenderCheckbox`…)
// rendent tous. Les *valeurs* du paquet (`theme.ColorText`, `theme.IconOK`) ne
// sont pas des appels et restent permises.
func renderingCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if sel.Sel.Name == "Render" {
		return "un appel à .Render()", true
	}
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "theme" {
		return "theme." + sel.Sel.Name + "()", true
	}
	return "", false
}

type varInit struct {
	name  string
	where string
	value ast.Expr
}

// packageLevelVarInits rend toutes les expressions d'initialisation des
// variables déclarées au niveau d'un paquet. Les déclarations à l'intérieur
// d'une fonction sont hors sujet : elles s'exécutent quand la fonction est
// appelée, pas à l'init.
func packageLevelVarInits(t *testing.T) []varInit {
	t.Helper()

	seen := map[string]bool{}
	var inits []varInit

	for _, tree := range scannedTrees {
		// La racine est résolue en absolu avant le parcours : `filepath.WalkDir`
		// visite l'entrée de départ elle-même, et le `..` d'un chemin relatif se
		// ferait alors prendre pour un répertoire caché par le filtre ci-dessous.
		root, err := filepath.Abs(tree)
		if err != nil {
			t.Fatalf("abs %s: %v", tree, err)
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Ni les dépendances, ni le git, ni les caches de build.
				name := d.Name()
				if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "bin") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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

			for _, d := range file.Decls {
				gen, ok := d.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, value := range vs.Values {
						name := "?"
						if i < len(vs.Names) {
							name = vs.Names[i].Name
						}
						inits = append(inits, varInit{
							name:  name,
							where: filepath.ToSlash(path) + ":" + strconv.Itoa(fset.Position(value.Pos()).Line),
							value: value,
						})
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(inits) == 0 {
		t.Fatal("aucune variable de paquet trouvée — le parcours des sources est cassé")
	}
	return inits
}
