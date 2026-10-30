package command

import (
	"maps"
	"sort"
	"strings"
)

// ViewType represents the view type
type ViewType string

const (
	ViewDashboard ViewType = "dashboard"
	ViewStatus    ViewType = "status"
	// ViewGitAuth and ViewGitExplorer are named after the *role*, not after a
	// platform: a context targets one forge (§3.6), so there is one
	// authentication screen and one explorer, and both adapt to whichever it
	// is. A `gitlab-` prefix would have to be typed as `github-` half the time
	// for the same view.
	ViewGitAuth      ViewType = "git-auth"
	ViewGitExplorer  ViewType = "git-explorer"
	ViewWorkspaces   ViewType = "workspaces"
	ViewSecurity     ViewType = "security"
	ViewContainers   ViewType = "containers"
	ViewOCIResources ViewType = "oci-resources"
	ViewNetdiag      ViewType = "netdiag"
	// ViewAgentUsage lists the coding-agent sessions found on disk. A view, not
	// a key, for the reason ViewJobs is.
	ViewAgentUsage    ViewType = "agent-usage"
	ViewConfiguration ViewType = "configuration"

	// ViewJobs lists what is running and what has run this session. It is
	// typed like any other view rather than reached by an uppercase key: the
	// uppercase vocabulary is for actions *within* a view, and changing view
	// goes through `:` (Rule 111).
	ViewJobs ViewType = "jobs"

	// ViewTemplates is the catalog of repository templates: what a new
	// repository can be filled from, and where each one lives. A view for the
	// reason ViewJobs is — changing view goes through `:` (Rule 111).
	ViewTemplates ViewType = "templates"

	// ViewAbout says which build is running and where it keeps its files. It is
	// a view rather than an uppercase key for the reason ViewJobs is: the
	// uppercase vocabulary acts on a resource *within* a screen, and this one
	// is a screen. It is also why it is not an overlay like `?` — the help
	// answers "what can I press here", which is a different question and one
	// that every view answers for itself.
	ViewAbout ViewType = "about"

	// ViewFiles browses the whole filesystem (§3.95). It is also what the
	// router lends a form's Browse button, as a picker.
	ViewFiles ViewType = "files"

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

// CommandType represents the command type
type CommandType string

const (
	CommandView    CommandType = "view"
	CommandContext CommandType = "context"
	CommandQuit    CommandType = "quit"
	CommandUnknown CommandType = "unknown"
)

// Command represents a parsed command
type Command struct {
	Type CommandType
	View ViewType // For view commands
	Args []string // For context commands
}

// viewNames maps every accepted spelling to the view it names. The key equal to
// the ViewType is its full name; every other key is an alias. It is derived
// from the registry (registry.go), the single source for parsing and
// completion: they used to be separate lists and had drifted (§1.3 D16, D17).
var viewNames = buildViewNames()

// resolveView looks a spelling up in viewNames, the single source for both
// parsing and completion.
func resolveView(name string) (ViewType, bool) {
	view, ok := viewNames[name]
	return view, ok
}

// actionNames are the commands that do not name a view. They take arguments
// (`context work`) or none at all (`quit`). Derived from the registry.
var actionNames = buildActionNames()

// actionAliases maps a short form to the full command in actionNames. Kept
// apart from viewNames because completion displays the two differently.
// Derived from the registry.
var actionAliases = buildActionAliases()

// normalize strips the leading colon and the case, so `:Status ` and `status`
// are the same command.
func normalize(input string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input), ":"))
}

// ParseCommand parses a command and returns a structured Command
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

// Parse resolves a view name alone, without action commands. This is the form
// used to read `default_view` in configuration.
// Returns an empty view, without error, when the name is not recognized.
func Parse(input string) (ViewType, error) {
	if view, ok := resolveView(normalize(input)); ok {
		return view, nil
	}
	return "", nil
}

// GetAliases returns every alias with the full name it abbreviates, views and
// action commands combined.
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

// ViewNames lists the canonical view names, sorted.
//
// FullNames() is not the same list: it also carries the action commands
// (`context`, `quit`), which is right for completion and wrong for
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

// FullNames returns the full name of each command — a view or an action —
// without the aliases, in registry order. This is what completion offers first.
func FullNames() []string {
	names := make([]string, len(registry))
	for i, s := range registry {
		names[i] = s.Name
	}
	return names
}
