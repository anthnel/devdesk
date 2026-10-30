package command

// EntryKind says whether an entry opens a view or runs an action.
type EntryKind int

const (
	KindView EntryKind = iota
	KindAction
)

// MaxDescriptionLen is the longest description, in runes.
const MaxDescriptionLen = 45

// Entry is one command target with all its spellings.
type Entry struct {
	Name        string
	Aliases     []string
	Description string
	// Arg is the argument placeholder an action takes, e.g. "<name>"; empty
	// when the entry takes none.
	Arg  string
	Kind EntryKind
}

// Label is what a listing shows for the entry: the name, followed by its
// argument placeholder when it takes one.
func (e Entry) Label() string {
	if e.Arg == "" {
		return e.Name
	}
	return e.Name + " " + e.Arg
}

// TakesArg reports whether the entry expects an argument.
func (e Entry) TakesArg() bool { return e.Arg != "" }

// spec is a registry row: the public Entry plus what the parser resolves it to.
type spec struct {
	Entry
	view   ViewType    // KindView only
	action CommandType // KindAction only
}

// registry is the single source of every command spelling: parsing
// (viewNames, actionNames, actionAliases), completion and listings all derive
// from it. Views come first, then actions, each alphabetical by canonical name;
// aliases keep their declaration order. ViewViewer is deliberately absent, for
// the reason given on its declaration.
var registry = []spec{
	{Entry{Name: "about", Aliases: []string{"version"}, Description: "Build version and where files are kept"}, ViewAbout, ""},
	{Entry{Name: "agent-usage", Aliases: []string{"au"}, Description: "Claude Code sessions and token usage"}, ViewAgentUsage, ""},
	{Entry{Name: "configuration", Aliases: []string{"config", "cfg"}, Description: "Settings of the current context"}, ViewConfiguration, ""},
	{Entry{Name: "containers", Aliases: []string{"cont", "ct"}, Description: "Containers, metrics and logs"}, ViewContainers, ""},
	{Entry{Name: "dashboard", Aliases: []string{"dash", "d"}, Description: "Code, health, host and Docker at a glance"}, ViewDashboard, ""},
	{Entry{Name: "files", Aliases: []string{"fs"}, Description: "Browse the whole filesystem"}, ViewFiles, ""},
	{Entry{Name: "git-auth", Aliases: []string{"ga"}, Description: "Sign in to your forge"}, ViewGitAuth, ""},
	{Entry{Name: "git-explorer", Aliases: []string{"ge"}, Description: "Browse and clone forge projects"}, ViewGitExplorer, ""},
	{Entry{Name: "jobs", Aliases: []string{"j"}, Description: "Scans, syncs and pulls of this session"}, ViewJobs, ""},
	{Entry{Name: "netdiag", Aliases: []string{"net"}, Description: "Reachability, ports, interfaces, forwarding"}, ViewNetdiag, ""},
	{Entry{Name: "oci-resources", Aliases: []string{"oci"}, Description: "Images, networks, volumes and registries"}, ViewOCIResources, ""},
	{Entry{Name: "security", Aliases: []string{"sec"}, Description: "CVE, secret and license scans"}, ViewSecurity, ""},
	{Entry{Name: "status", Aliases: []string{"s"}, Description: "HTTP, ICMP, DNS and SSL checks"}, ViewStatus, ""},
	{Entry{Name: "templates", Aliases: []string{"tpl"}, Description: "Repository template catalog"}, ViewTemplates, ""},
	{Entry{Name: "workspaces", Aliases: []string{"ws", "w"}, Description: "Local repositories and their Git state"}, ViewWorkspaces, ""},
	{Entry{Name: "context", Aliases: []string{"ctx", "c"}, Description: "Switch or list contexts", Arg: "<name>", Kind: KindAction}, "", CommandContext},
	{Entry{Name: "quit", Aliases: []string{"q"}, Description: "Exit DevDesk", Kind: KindAction}, "", CommandQuit},
}

// Entries returns the ordered registry as a deep copy: callers may modify it
// freely.
func Entries() []Entry {
	out := make([]Entry, len(registry))
	for i, s := range registry {
		out[i] = s.Entry
		out[i].Aliases = append([]string(nil), s.Aliases...)
	}
	return out
}

// buildViewNames derives the spelling-to-view map from the registry.
func buildViewNames() map[string]ViewType {
	m := make(map[string]ViewType)
	for _, s := range registry {
		if s.Kind != KindView {
			continue
		}
		m[s.Name] = s.view
		for _, a := range s.Aliases {
			m[a] = s.view
		}
	}
	return m
}

// buildActionNames derives the action-name map from the registry.
func buildActionNames() map[string]CommandType {
	m := make(map[string]CommandType)
	for _, s := range registry {
		if s.Kind == KindAction {
			m[s.Name] = s.action
		}
	}
	return m
}

// buildActionAliases derives the action alias-to-name map from the registry.
func buildActionAliases() map[string]string {
	m := make(map[string]string)
	for _, s := range registry {
		if s.Kind != KindAction {
			continue
		}
		for _, a := range s.Aliases {
			m[a] = s.Name
		}
	}
	return m
}
