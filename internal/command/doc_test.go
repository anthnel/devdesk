package command

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Le vocabulaire des commandes est déjà tenu par deux tests qui opposent le
// parser à la complétion (TestEverythingThatParsesCanBeCompleted et son
// symétrique). Il leur manque le troisième bord : ce que la documentation
// annonce. Ces deux tests le ferment, sur le modèle d'internal/ui/keymap, qui
// oppose de la même façon un vocabulaire déclaré aux sources.
//
// Ce n'est pas théorique. `.claude/CLAUDE.md` a annoncé « `containers` or `c` »
// depuis le commit initial, alors que `c` a toujours résolu vers `context` —
// D16 était exactement ce défaut dans l'autre sens (`:netdiag` documenté,
// refusé par le parser), et il avait été trouvé par un test. Celui-ci a
// survécu à D16/D17 parce que les deux tests d'alors ne regardaient que le
// code.

// docPath est le chemin de CLAUDE.md depuis ce paquet.
func docPath() string {
	return filepath.Join("..", "..", ".claude", "CLAUDE.md")
}

// docListIntro est la ligne qui ouvre la liste. Les puces qui suivent, jusqu'à
// la première ligne vide, sont les commandes documentées.
const docListIntro = "Press `ctrl+p` to enter command mode, then type:"

var backticked = regexp.MustCompile("`([^`]+)`")

// documentedCommand est une puce de la liste : toutes les orthographes qu'elle
// annonce, et la ligne entière pour que l'échec soit lisible.
type documentedCommand struct {
	spellings []string
	line      string
}

// readDocumentedCommands lit les puces de la liste de CLAUDE.md.
func readDocumentedCommands(t *testing.T) []documentedCommand {
	t.Helper()

	raw, err := os.ReadFile(docPath())
	if err != nil {
		t.Fatalf("cannot read %s: %v", docPath(), err)
	}

	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == docListIntro {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s no longer contains the command list intro %q — the tests that read it have to be pointed at wherever it moved", docPath(), docListIntro)
	}

	var out []documentedCommand
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		var spellings []string
		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			spellings = append(spellings, m[1])
		}
		if len(spellings) > 0 {
			out = append(out, documentedCommand{spellings: spellings, line: strings.TrimSpace(line)})
		}
	}

	if len(out) == 0 {
		t.Fatalf("%s: the command list under %q is empty", docPath(), docListIntro)
	}
	return out
}

// bare enlève l'argument d'exemple d'une orthographe documentée : `context
// <name>` et `context list` nomment tous deux la commande `context`.
func bare(spelling string) string {
	head, _, _ := strings.Cut(spelling, " ")
	return head
}

// TestEveryDocumentedCommandParsesToWhatItClaims oppose CLAUDE.md au parser :
// chaque orthographe annoncée doit être acceptée, et toutes celles d'une même
// puce doivent mener au même endroit — c'est la puce qui affirme qu'elles sont
// synonymes.
func TestEveryDocumentedCommandParsesToWhatItClaims(t *testing.T) {
	for _, doc := range readDocumentedCommands(t) {
		first := ParseCommand(bare(doc.spellings[0]))
		if first.Type == CommandUnknown {
			t.Errorf("%s\n  %q is documented but the parser refuses it", doc.line, doc.spellings[0])
			continue
		}

		for _, spelling := range doc.spellings[1:] {
			got := ParseCommand(bare(spelling))
			if got.Type == CommandUnknown {
				t.Errorf("%s\n  %q is documented but the parser refuses it", doc.line, spelling)
				continue
			}
			if got.Type != first.Type || got.View != first.View {
				t.Errorf("%s\n  %q resolves to %s/%s, %q to %s/%s — the line says they are the same command",
					doc.line, doc.spellings[0], first.Type, first.View, spelling, got.Type, got.View)
			}
		}
	}
}

// TestEveryTypeableViewIsDocumented ferme le sens inverse, celui de D16 : une
// vue qu'on peut atteindre au clavier et que la documentation ne nomme pas est
// une vue que personne ne trouvera.
//
// Seuls les noms complets sont exigés. Les alias sont un choix éditorial — la
// liste en montre un ou deux par vue et n'a pas à les porter tous — alors qu'un
// nom complet manquant est une vue absente de la documentation.
func TestEveryTypeableViewIsDocumented(t *testing.T) {
	documented := map[string]bool{}
	for _, doc := range readDocumentedCommands(t) {
		for _, spelling := range doc.spellings {
			documented[bare(spelling)] = true
		}
	}

	for _, name := range ViewNames() {
		if !documented[name] {
			t.Errorf("view %q can be typed but %s does not list it", name, docPath())
		}
	}
}
