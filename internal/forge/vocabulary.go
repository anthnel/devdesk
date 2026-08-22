package forge

import "github.com/anthnel/devdesk/internal/config"

// Vocabulary is what a forge is *called*, as opposed to what it can do.
//
// The distinction is §3.6's: "Group" against "Organization" is a **word**,
// while nesting depth and the visibility set are **shapes** — they change what
// the application can promise, and no wording helps with them. Shape carries
// the second; this carries the first, and neither grows conditionals for the
// other.
//
// # Per forge, not neutral
//
// A GitLab user says *group*, a GitHub user says *repository*. "Namespace" is a
// third language nobody speaks, and it makes the application read as an
// abstraction layer rather than as a tool. So every word here is the one its
// forge's own documentation uses.
//
// The field names are neutral because the *code* has to be — a view cannot
// switch on which forge it is talking to — but nothing a user reads is.
//
// # No view writes a forge's name into a string
//
// That is the rule this type exists to make checkable, and
// TestNoViewNamesAForge (internal/ui/vocabtest) walks every string literal
// under internal/ui to enforce it. A command name is not covered: it is routing
// identity, the same for both forges, and lives in internal/command.
type Vocabulary struct {
	// Name is the forge as it names itself — "GitLab", "GitHub". Capitalised:
	// it is a proper noun, and it is what titles and messages interpolate.
	Name string

	// Namespace holds repositories and, on some forges, other namespaces.
	Namespace, Namespaces string

	// Repository is one repository.
	Repository, Repositories string

	// ChangeRequest is a proposal to merge a branch.
	ChangeRequest, ChangeRequests string

	// ChangeRequestShort is the initialism — "MR", "PR". The dashboard's tree
	// nodes are eleven cells wide, so the long form does not fit and truncating
	// it would produce "Merge Req…" on one forge and "Pull Requ…" on the other.
	ChangeRequestShort string

	// TokenLabel is what the auth view calls the secret it asks for.
	TokenLabel string

	// TokenPlaceholder is an example token, shown greyed in the input until the
	// user types.
	//
	// It is a hint and never a check: DevDesk does not validate the prefix, and
	// a forge that changes its format must not turn a working token into a
	// refused one. Shaped like a real value rather than as prose, because that
	// is what a placeholder is — the prose about which prefixes exist lives in
	// TokenHelp, where there is room to name more than one.
	TokenPlaceholder string

	// TokenHelp says where to create a token and which scopes it needs. One
	// sentence, because it is rendered in a help pane beside others.
	TokenHelp string

	// ExampleURL is what the configuration view suggests as a host. The public
	// instance, because it is the only address that is the same for everyone —
	// a self-hosted example would be a hostname nobody has.
	ExampleURL string
}

// gitlabVocabulary and githubVocabulary are the two, written out rather than
// derived: every word is a decision about what that forge's users call things,
// and a rule that produced them would be a rule about English rather than about
// either product.
var (
	gitlabVocabulary = Vocabulary{
		Name:               "GitLab",
		Namespace:          "Group",
		Namespaces:         "Groups",
		Repository:         "Project",
		Repositories:       "Projects",
		ChangeRequest:      "Merge Request",
		ChangeRequests:     "Merge Requests",
		ChangeRequestShort: "MR",
		TokenLabel:         "Personal Access Token",
		TokenPlaceholder:   "glpat-…",
		TokenHelp:          "Create a token in GitLab > Settings > Access Tokens. Required scopes: api, read_user.",
		ExampleURL:         "https://gitlab.com",
	}

	githubVocabulary = Vocabulary{
		Name:               "GitHub",
		Namespace:          "Organization",
		Namespaces:         "Organizations",
		Repository:         "Repository",
		Repositories:       "Repositories",
		ChangeRequest:      "Pull Request",
		ChangeRequests:     "Pull Requests",
		ChangeRequestShort: "PR",
		TokenLabel:         "Personal Access Token",
		TokenPlaceholder:   "ghp_… or github_pat_…",
		TokenHelp:          "Create a token in GitHub > Settings > Developer settings > Personal access tokens. Required scopes: repo, read:org.",
		ExampleURL:         "https://github.com",
	}
)

// VocabularyFor resolves the wording for a configured forge type.
//
// It takes the **configured** type rather than a live Forge, and that is
// deliberate: the explorer's "not authenticated" screen and the auth view's own
// title both need the words before any session exists. A method on Forge would
// be unavailable exactly where the words are needed most.
//
// An unknown type falls back to GitLab rather than to an empty Vocabulary. A
// screen with no nouns on it is a worse failure than a screen using the wrong
// forge's nouns, and config.applyDefaults already guarantees a known value —
// this is the belt to that brace.
func VocabularyFor(forgeType string) Vocabulary {
	switch forgeType {
	case config.ForgeGitHub:
		return githubVocabulary
	default:
		return gitlabVocabulary
	}
}
