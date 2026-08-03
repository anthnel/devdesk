package command

import (
	"maps"
	"strings"
)

// ViewType représente le type de vue
type ViewType string

const (
	ViewDashboard      ViewType = "dashboard"
	ViewStatus         ViewType = "status"
	ViewGitlabAuth     ViewType = "gitlab-auth"
	ViewGitlabExplorer ViewType = "gitlab-explorer"
	ViewWorkspaces     ViewType = "workspaces"
	ViewSecurity       ViewType = "security"
	ViewContainers     ViewType = "containers"
	ViewOCIResources   ViewType = "oci-resources"
	ViewNetdiag        ViewType = "netdiag"
)

// CommandType représente le type de commande
type CommandType string

const (
	CommandView    CommandType = "view"
	CommandContext CommandType = "context"
	CommandTheme   CommandType = "theme"
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
	"dashboard":       ViewDashboard,
	"dash":            ViewDashboard,
	"d":               ViewDashboard,
	"status":          ViewStatus,
	"s":               ViewStatus,
	"gitlab-auth":     ViewGitlabAuth,
	"gla":             ViewGitlabAuth,
	"gitlab-explorer": ViewGitlabExplorer,
	"gle":             ViewGitlabExplorer,
	"explorer":        ViewGitlabExplorer,
	"exp":             ViewGitlabExplorer,
	"workspaces":      ViewWorkspaces,
	"ws":              ViewWorkspaces,
	"w":               ViewWorkspaces,
	"security":        ViewSecurity,
	"sec":             ViewSecurity,
	"containers":      ViewContainers,
	"cont":            ViewContainers,
	"ct":              ViewContainers,
	"oci-resources":   ViewOCIResources,
	"oci":             ViewOCIResources,
	"netdiag":         ViewNetdiag,
	"net":             ViewNetdiag,
}

// actionNames are the commands that do not name a view. They take arguments
// (`context work`, `theme mocha`) or none at all (`quit`).
var actionNames = map[string]CommandType{
	"context": CommandContext,
	"theme":   CommandTheme,
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

	if view, ok := viewNames[mainCmd]; ok {
		return Command{Type: CommandView, View: view}
	}

	return Command{Type: CommandUnknown}
}

// Parse résout un nom de vue seul, sans les commandes d'action. C'est la forme
// utilisée pour lire `default_view` en configuration.
// Retourne une vue vide, sans erreur, quand le nom n'est pas reconnu.
func Parse(input string) (ViewType, error) {
	if view, ok := viewNames[normalize(input)]; ok {
		return view, nil
	}
	return "", nil
}

// GetAliases retourne chaque alias avec le nom complet qu'il abrège, vues et
// commandes d'action confondues.
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
