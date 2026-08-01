package command

import (
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
	ViewNet            ViewType = "net"
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

// ParseCommand analyse une commande et retourne un Command structuré
func ParseCommand(input string) Command {
	// Nettoyer l'input
	cmd := strings.TrimSpace(input)
	cmd = strings.TrimPrefix(cmd, ":")
	cmd = strings.ToLower(cmd)

	// Séparer en parties
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return Command{Type: CommandUnknown}
	}

	mainCmd := parts[0]
	args := parts[1:]

	// Vérifier les commandes quit
	if mainCmd == "quit" || mainCmd == "q" {
		return Command{Type: CommandQuit}
	}

	// Vérifier les commandes context
	if mainCmd == "context" || mainCmd == "ctx" || mainCmd == "c" {
		return Command{
			Type: CommandContext,
			Args: args,
		}
	}

	// Vérifier les commandes theme
	if mainCmd == "theme" {
		return Command{
			Type: CommandTheme,
			Args: args,
		}
	}

	// Vérifier les commandes de vue
	viewMap := map[string]ViewType{
		"dashboard":       ViewDashboard,
		"dash":            ViewDashboard,
		"d":               ViewDashboard,
		"status":          ViewStatus,
		"s":               ViewStatus,
		"gitlab-auth":     ViewGitlabAuth,
		"gla":             ViewGitlabAuth,
		"gitlab-explorer": ViewGitlabExplorer,
		"explorer":        ViewGitlabExplorer,
		"exp":             ViewGitlabExplorer,
		"workspaces":      ViewWorkspaces,
		"ws":              ViewWorkspaces,
		"security":        ViewSecurity,
		"sec":             ViewSecurity,
		"containers":      ViewContainers,
		"cont":            ViewContainers,
		"ct":              ViewContainers,
		"oci-resources":   ViewOCIResources,
		"oci":             ViewOCIResources,
		"net":             ViewNet,
	}

	if view, ok := viewMap[mainCmd]; ok {
		return Command{
			Type: CommandView,
			View: view,
		}
	}

	return Command{Type: CommandUnknown}
}

// Parse analyse une commande saisie en mode commande (backward compatible)
// Retourne le ViewType correspondant ou une erreur
func Parse(input string) (ViewType, error) {
	// Nettoyer l'input
	cmd := strings.TrimSpace(input)
	cmd = strings.TrimPrefix(cmd, ":")
	cmd = strings.ToLower(cmd)

	// Mapping des commandes vers les vues
	commands := map[string]ViewType{
		"dashboard":       ViewDashboard,
		"status":          ViewStatus,
		"gitlab-auth":     ViewGitlabAuth,
		"gitlab-explorer": ViewGitlabExplorer,
		"workspaces":      ViewWorkspaces,
		"security":        ViewSecurity,

		// Aliases
		"dash":          ViewDashboard,
		"d":             ViewDashboard,
		"s":             ViewStatus,
		"gla":           ViewGitlabAuth,
		"explorer":      ViewGitlabExplorer,
		"exp":           ViewGitlabExplorer,
		"ws":            ViewWorkspaces,
		"sec":           ViewSecurity,
		"containers":    ViewContainers,
		"cont":          ViewContainers,
		"ct":            ViewContainers,
		"oci-resources": ViewOCIResources,
		"oci":           ViewOCIResources,
		"net":           ViewNet,
	}

	if view, ok := commands[cmd]; ok {
		return view, nil
	}

	// Commande non reconnue, retourner une vue vide
	return "", nil
}

// GetAliases retourne les alias disponibles
func GetAliases() map[string]string {
	return map[string]string{
		"dash":     "dashboard",
		"d":        "dashboard",
		"s":        "status",
		"gla":      "gitlab-auth",
		"explorer": "gitlab-explorer",
		"exp":      "gitlab-explorer",
		"ws":       "workspaces",
		"sec":      "security",
		"cont":     "containers",
		"ct":       "containers",
		"oci":      "oci-resources",
	}
}
