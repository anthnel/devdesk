package command

import (
	"maps"
	"sort"
	"strings"
)

// ViewType représente le type de vue
type ViewType string

const (
	ViewDashboard ViewType = "dashboard"
	ViewStatus    ViewType = "status"
	// ViewGitAuth and ViewGitExplorer are named after the *role*, not after a
	// platform: a context targets one forge (§3.6), so there is one
	// authentication screen and one explorer, and both adapt to whichever it
	// is. A `gitlab-` prefix would have to be typed as `github-` half the time
	// for the same view.
	ViewGitAuth       ViewType = "git-auth"
	ViewGitExplorer   ViewType = "git-explorer"
	ViewWorkspaces    ViewType = "workspaces"
	ViewSecurity      ViewType = "security"
	ViewContainers    ViewType = "containers"
	ViewOCIResources  ViewType = "oci-resources"
	ViewNetdiag       ViewType = "netdiag"
	ViewConfiguration ViewType = "configuration"

	// ViewViewer is opened by the router on another view's request — a file in
	// workspaces, an inspect or a log in containers — and never by name. It is
	// deliberately absent from viewNames: `:viewer` would open a pane saying
	// there is nothing in it, and app.default_view would offer it as a landing
	// view, which is the defect ViewNames() was split from FullNames() to fix.
	ViewViewer ViewType = "viewer"
)

// routerViews are the views the router opens itself. They are real views with
// real headers and help, so the contract tests have to reach them — see
// AllViewNames — but nothing a user can type resolves to one.
var routerViews = []ViewType{ViewViewer}

// CommandType représente le type de commande
type CommandType string

const (
	CommandView    CommandType = "view"
	CommandContext CommandType = "context"
	CommandQuit    CommandType = "quit"
	CommandUnknown CommandType = "unknown"
)

// Command représente une commande parsée
type Command struct {
	Type CommandType
	View ViewType // Pour les commandes view
	Args []string // Pour les commandes context
}

// viewNames maps every accepted spelling to the view it names. The key equal to
// the ViewType is its full name; every other key is an alias.
//
// This map is the single source for both parsing and completion. They used to
// be separate lists and had drifted: four views could be typed in full but not
// tab-completed, and `netdiag` was documented everywhere while only `net`
// parsed (§1.3 D16, D17).
var viewNames = map[string]ViewType{
	"dashboard":     ViewDashboard,
	"dash":          ViewDashboard,
	"d":             ViewDashboard,
	"status":        ViewStatus,
	"s":             ViewStatus,
	"git-auth":      ViewGitAuth,
	"ga":            ViewGitAuth,
	"git-explorer":  ViewGitExplorer,
	"ge":            ViewGitExplorer,
	"workspaces":    ViewWorkspaces,
	"ws":            ViewWorkspaces,
	"w":             ViewWorkspaces,
	"security":      ViewSecurity,
	"sec":           ViewSecurity,
	"containers":    ViewContainers,
	"cont":          ViewContainers,
	"ct":            ViewContainers,
	"oci-resources": ViewOCIResources,
	"oci":           ViewOCIResources,
	"netdiag":       ViewNetdiag,
	"net":           ViewNetdiag,
	"configuration": ViewConfiguration,
	"config":        ViewConfiguration,
	"cfg":           ViewConfiguration,
}

// legacyNames are spellings that still resolve but are never suggested.
//
// They are the forge-prefixed names §3.6 replaced. Keeping them parseable is
// deliberately permissive: there is one authentication view, so `gla` typed out
// of habit should go there rather than fail, and punishing muscle memory buys
// nothing. Keeping them *unsuggested* is what makes the new names the ones a
// user learns — the completion list is the only place most people read them.
//
// The GitHub spellings were never accepted before and are here for the same
// reason as the GitLab ones: someone whose context targets GitHub will guess
// `gha` before `ga`, and being right is worth more than being consistent about
// what used to exist.
//
// **The filtering happens in completion, not in parsing.** That split is what
// makes the rename feel like a rename rather than a removal.
var legacyNames = map[string]ViewType{
	"gitlab-auth":     ViewGitAuth,
	"gla":             ViewGitAuth,
	"github-auth":     ViewGitAuth,
	"gha":             ViewGitAuth,
	"gitlab-explorer": ViewGitExplorer,
	"gle":             ViewGitExplorer,
	"github-explorer": ViewGitExplorer,
	"ghe":             ViewGitExplorer,

	// `explorer` and `exp` predate the prefix question entirely. They are
	// retired for the same reason: one short form per view, and `ge` is the one
	// that pairs with `ga`.
	"explorer": ViewGitExplorer,
	"exp":      ViewGitExplorer,
}

// resolveView looks a spelling up in both tables. Parsing sees them as one;
// only completion tells them apart.
func resolveView(name string) (ViewType, bool) {
	if view, ok := viewNames[name]; ok {
		return view, true
	}
	view, ok := legacyNames[name]
	return view, ok
}

// actionNames are the commands that do not name a view. They take arguments
// (`context work`, `theme mocha`) or none at all (`quit`).
var actionNames = map[string]CommandType{
	"context": CommandContext,
	"quit":    CommandQuit,
}

// actionAliases maps a short form to the full command in actionNames. Kept
// apart from viewNames because completion displays the two differently.
var actionAliases = map[string]string{
	"ctx": "context",
	"c":   "context",
	"q":   "quit",
}

// normalize strips the leading colon and the case, so `:Status ` and `status`
// are the same command.
func normalize(input string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input), ":"))
}

// ParseCommand analyse une commande et retourne un Command structuré
func ParseCommand(input string) Command {
	parts := strings.Fields(normalize(input))
	if len(parts) == 0 {
		return Command{Type: CommandUnknown}
	}

	mainCmd, args := parts[0], parts[1:]
	if full, isAlias := actionAliases[mainCmd]; isAlias {
		mainCmd = full
	}

	if action, ok := actionNames[mainCmd]; ok {
		if action == CommandQuit {
			return Command{Type: CommandQuit}
		}
		return Command{Type: action, Args: args}
	}

	if view, ok := resolveView(mainCmd); ok {
		return Command{Type: CommandView, View: view}
	}

	return Command{Type: CommandUnknown}
}

// Parse résout un nom de vue seul, sans les commandes d'action. C'est la forme
// utilisée pour lire `default_view` en configuration.
// Retourne une vue vide, sans erreur, quand le nom n'est pas reconnu.
func Parse(input string) (ViewType, error) {
	if view, ok := resolveView(normalize(input)); ok {
		return view, nil
	}
	return "", nil
}

// GetAliases retourne chaque alias avec le nom complet qu'il abrège, vues et
// commandes d'action confondues.
//
// legacyNames is deliberately absent: this feeds completion, and a spelling
// that parses without being suggested is exactly what those are.
func GetAliases() map[string]string {
	aliases := make(map[string]string, len(viewNames)+len(actionAliases))
	for name, view := range viewNames {
		if name != string(view) {
			aliases[name] = string(view)
		}
	}
	maps.Copy(aliases, actionAliases)
	return aliases
}

// FullNames retourne le nom complet de chaque commande — une vue ou une action
// — sans les alias. C'est ce que la complétion propose en premier.
// ViewNames lists the canonical view names, sorted.
//
// FullNames() is not the same list: it also carries the action commands
// (`context`, `theme`, `quit`), which is right for completion and wrong for
// anything that means "a view". app.default_view is the case that showed it —
// offering `quit` as a landing view.
func ViewNames() []string {
	names := make([]string, 0, len(viewNames))
	for name, view := range viewNames {
		if name == string(view) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// AllViewNames lists every view the application can put on screen, typeable or
// not, sorted.
//
// It is what the router's contract tests iterate. ViewNames() answers "what can
// a user type", which is the right question for completion and for
// app.default_view and the wrong one for "does every view supply a header and
// help" — a router-only view renders in the same viewport as all the others and
// fails the same way when it does not.
func AllViewNames() []string {
	names := ViewNames()
	for _, view := range routerViews {
		names = append(names, string(view))
	}
	sort.Strings(names)
	return names
}

func FullNames() []string {
	names := make([]string, 0, len(viewNames)+len(actionNames))
	for name, view := range viewNames {
		if name == string(view) {
			names = append(names, name)
		}
	}
	for name := range actionNames {
		names = append(names, name)
	}
	return names
}
