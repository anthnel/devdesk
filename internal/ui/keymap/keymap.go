// Package keymap déclare le vocabulaire clavier de l'application.
//
// Il ne lit aucune touche et ne dépend pas de bubbletea : c'est une liste de
// chaînes, et son seul rôle est d'être la référence contre laquelle les tests
// opposent le code (keymap_test.go). Sans cette référence, la cohérence des
// touches est une convention que rien ne vérifie — ce qui est exactement l'état
// que §3.26 a relevé : 184 liaisons, 16 collisions.
//
// # Trois espaces de noms disjoints
//
//   - Une MAJUSCULE est une action, et son sens est global à l'application.
//     `D` supprime, dans toutes les vues, quoi que « supprimer » veuille dire
//     là où on se trouve.
//   - Une minuscule est un filtre ou une bascule d'affichage. Elle ne modifie
//     rien, donc son sens peut être local et deux vues peuvent employer la même
//     lettre sans se contredire.
//   - Le reste (flèches, esc, enter, tab, space, `/`, `.`, `?`, `:`) est
//     structurel et ne change jamais.
//
// # Pourquoi Shift porte les actions
//
// Ce n'est pas un goût, c'est le budget réel d'un terminal :
//
//	Ctrl+lettre  : n'encode que l'ASCII 0x40-0x5F, et le tty en confisque
//	               quatre (ctrl+i = TAB, ctrl+m = Entrée, ctrl+j = LF,
//	               ctrl+h = Backspace). ctrl+a et ctrl+b sont les préfixes de
//	               screen et tmux, ctrl+c/ctrl+d sont SIGINT et EOF,
//	               ctrl+s/ctrl+q le contrôle de flux.        → ~14 restantes
//	Alt+touche   : Option n'est pas Meta sur macOS tant que l'utilisateur ne
//	               l'active pas ; l'application ne reçoit rien.        → 0
//	Ctrl+Shift   : le code de contrôle écrase la casse — ctrl+a et ctrl+shift+a
//	               émettent tous deux 0x01. Les distinguer exige le protocole
//	               clavier Kitty ou modifyOtherKeys, que bubbletea v1.3.10
//	               n'active pas (c'est WithKeyboardEnhancements() en v2). Et
//	               même alors l'émulateur se sert d'abord : ctrl+shift+c/v/t/w/n
//	               sont copier, coller, onglet, fermer, fenêtre.       → 0
//	Shift+lettre : aucune contrainte. Passe sur tout émulateur, toute
//	               plateforme, à travers SSH et tmux.                  → 26
//
// Shift+lettre EST une combinaison à deux touches : deux doigts, aucun départ
// accidentel. Elle porte le vocabulaire non par défaut, mais parce que les deux
// autres familles sont amputées ou inutilisables.
package keymap

import (
	"maps"
	"sort"
	"strings"
)

// Les 21 actions. Une quarantaine d'actions existaient pour 26 lettres : la
// règle ne tient qu'après fusion des synonymes (Kill = arrêter et tuer,
// Delete = supprimer et retirer, Terminal = terminal et shell) et parce que les
// bascules d'affichage sortent du compte.
const (
	New      = "N" // Créer une ressource depuis ce contexte
	Edit     = "E" // Éditer la ressource sélectionnée
	Delete   = "D" // Supprimer la ressource sélectionnée
	Rename   = "M" // Renommer (mv)
	Scan     = "S" // Scanner la cible sélectionnée
	ScanAll  = "A" // Scanner tout (modale : case « purger le cache d'abord »)
	Fetch    = "F" // Se remettre au niveau de la source : fetch + fast-forward, ou suivre un flux
	Clone    = "C" // Entrer en sélection de clone
	Terminal = "T" // Ouvrir un terminal ou un shell
	IDE      = "O" // Ouvrir dans l'IDE configuré
	Web      = "W" // Ouvrir une URL dans le navigateur
	Logs     = "L" // Ouvrir les logs
	Pager    = "V" // Ouvrir dans le pager système
	Kill     = "K" // Arrêter, tuer (modale : Stop / Restart, ou SIGKILL)
	Prune    = "P" // Supprimer les ressources inutilisées
	Browser  = "B" // Ouvrir le navigateur multi-registries
	Get      = "G" // Pull (get) l'image ou le tag
	Auth     = "U" // Login / logout registry — bascule sur l'état de la ligne
	Exclude  = "X" // Exclure — ajouter à .gitleaksignore
	Requests = "R" // Ouvrir les merge requests · PR
	Issues   = "I" // Ouvrir les issues
)

// CommandMode ouvre la ligne de commande, depuis n'importe où — y compris
// depuis un champ texte focusé.
//
// ctrl+p, et pas ctrl+: : ":" vaut 0x3A, hors de la plage 0x40-0x5F que Ctrl
// encode, donc "ctrl+:" n'atteint jamais l'application. Le repli était alt+:,
// qui a le défaut inverse : sur Terminal.app et iTerm2, Option+Shift+; émet un
// caractère littéral et la touche n'arrive pas non plus. ctrl+p est libre dans
// toute l'application, n'est aucun caractère de contrôle du tty, n'est le
// préfixe d'aucun multiplexeur, et son sens est déjà appris — palette.
const CommandMode = "ctrl+p"

// actions associe chaque touche d'action à son sens. C'est la table que les
// tests opposent au code ; le tableau de §3.26 en est la forme lisible.
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
}

// Actions rend une copie de la table touche → sens.
func Actions() map[string]string {
	out := make(map[string]string, len(actions))
	maps.Copy(out, actions)
	return out
}

// IsAction dit si une touche appartient au vocabulaire d'actions.
func IsAction(key string) bool {
	_, ok := actions[key]
	return ok
}

// modalKeys sont les raccourcis d'une modale de confirmation.
//
// C'est un quatrième espace de noms, et il est disjoint des trois autres par le
// mode plutôt que par la casse : une modale réclame toute touche avant que la
// vue ne la voie (priorité 1 dans chaque handleKeyMsg), donc `N` n'y peut pas
// vouloir dire « créer » — la vue en dessous ne reçoit rien. C'est le même
// argument que pour InEditMode.
//
// Ils sont déclarés ici parce que le relevé de §3.26 ne les avait pas vus, et
// qu'une majuscule liée sans déclaration est indistinguable d'une dérive.
var modalKeys = map[string]string{
	"y": "Yes", "Y": "Yes",
	"n": "No", "N": "No",
}

// ModalKeys rend les raccourcis de modale.
func ModalKeys() map[string]string {
	out := make(map[string]string, len(modalKeys))
	maps.Copy(out, modalKeys)
	return out
}

// IsModalKey dit si une touche est un raccourci de modale.
func IsModalKey(key string) bool {
	_, ok := modalKeys[key]
	return ok
}

// free liste les majuscules qu'aucune action n'occupe. Elles sont déclarées
// plutôt que laissées à déduire : le prochain ajout doit savoir où piocher sans
// refaire le relevé, et une action qui s'installe ailleurs qu'ici est un
// doublon qui s'ignore.
//
// `Y` y figure bien qu'une modale l'emploie : une action `Y` et le raccourci
// « Yes » ne sont jamais joignables en même temps.
var free = []string{"H", "J", "Q", "Y", "Z"}

// Free rend les majuscules encore disponibles, triées.
func Free() []string {
	out := append([]string(nil), free...)
	sort.Strings(out)
	return out
}

// Surface est un écran, ses fichiers, et les minuscules qu'il emploie.
//
// Path est ce qui rattache un fichier à sa surface, et il est là pour que la
// déclaration soit vérifiable : sans lui, « ces lettres sont locales à cette
// vue » se contrôle en réunissant toutes les listes, et `l` déclaré pour
// netdiag excuserait `l` dans les registries. La correspondance se fait sur le
// préfixe le plus long, donc netdiag/ports peut restreindre netdiag.
type Surface struct {
	Name string
	Path string
	Keys []string
}

// localToggles recense les minuscules employées comme bascules d'affichage ou
// de filtre. Elles ne modifient rien, donc leur sens est local et la même
// lettre peut servir deux fois sans se contredire — `l` est le protocole dans
// netdiag et la sévérité LOW dans security.
//
// Un fichier qui ne correspond à aucune surface n'a droit à aucune minuscule :
// c'est le défaut, et c'est ce qui fait que la liste doit être tenue.
var localToggles = []Surface{
	{"containers", "ui/containers/", []string{"a"}},
	{"viewer", "ui/viewer/", []string{"f", "c", "w", "v", "t"}},
	{"netdiag/ports", "ui/netdiag/ports_model.go", []string{"t", "u", "l", "e", "n", "z"}},
	{"netdiag/detail", "ui/netdiag/", []string{"f"}},
	{"oci/browser", "ui/oci_resources/browser_", []string{"r"}},
	{"security/findings", "ui/security/", []string{"c", "h", "m", "l"}},
}

// LocalToggles rend les surfaces déclarées.
func LocalToggles() []Surface {
	return append([]Surface(nil), localToggles...)
}

// SurfaceFor rattache un chemin de fichier à sa surface, par préfixe le plus
// long. Le second retour dit si une surface a été trouvée.
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

// Exception est une touche qui déroge au vocabulaire, avec sa raison.
type Exception struct {
	Key     string
	Surface string
	Why     string
}

// exceptions sont les deux dérogations assumées. Elles sont écrites ici parce
// que §3.26 l'exige : une exception non déclarée est indistinguable d'une
// dérive, et le prochain relevé la « corrigerait ».
var exceptions = []Exception{
	{
		Key:     "c",
		Surface: "oci/network-inspect",
		Why: "Connectivity test. Brûler une majuscule globale pour une action " +
			"présente dans un seul sous-écran à faible densité coûterait plus " +
			"que ça ne rapporte.",
	},
	{
		Key:     "ctrl+y",
		Surface: "oci/launch-form",
		Why: "Copier la commande docker run. Même raison, et ctrl+y ne heurte " +
			"aucun caractère de contrôle utilisé ailleurs.",
	},
}

// DeclaredExceptions rend les dérogations déclarées.
func DeclaredExceptions() []Exception {
	return append([]Exception(nil), exceptions...)
}

// IsException dit si une touche est une dérogation déclarée sur une surface.
func IsException(key string) bool {
	for _, e := range exceptions {
		if e.Key == key {
			return true
		}
	}
	return false
}
