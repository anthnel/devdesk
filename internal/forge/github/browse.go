package github

import (
	"context"
	"strings"

	gh "github.com/google/go-github/v68/github"

	"github.com/anthnel/devdesk/internal/forge"
)

// perPage is what every list endpoint asks for. GitHub caps it at 100.
const perPage = 100

// listAll walks every page and returns the lot — D34's rule, applied to the
// second backend before it can go wrong here too.
//
// GitHub paginates by Link header, which go-github surfaces as
// Response.NextPage, 0 on the last page. The guard against a server that keeps
// pointing at the page just served is the GitLab backend's, for the same
// reason: it cannot cut a legitimate response short, which a page cap would.
func listAll[T any](fetch func(opts *gh.ListOptions) ([]T, *gh.Response, error)) ([]T, error) {
	opts := &gh.ListOptions{PerPage: perPage, Page: 1}

	var all []T
	for {
		page, resp, err := fetch(opts)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)

		if resp == nil || resp.NextPage <= opts.Page {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// RootNamespaces lists the user's own account, then the organisations the token
// can see.
//
// **The personal account comes first, and leaving it out was a bug.** The first
// version of this excluded it, reasoning that "a personal namespace is not an
// organisation: it cannot be created or deleted, so listing it would put a row
// in the tree that half the actions refuse". That is wrong on its own terms —
// *no* GitHub organisation can be created or deleted through the API, so the
// personal account is not less capable than an organisation but **more**: it is
// the one namespace where a repository can be created and deleted.
//
// The cost of the mistake was the common case. A personal GitHub account
// belongs to no organisation, so the explorer opened empty and said so, on the
// account shape most people have.
func (f *Forge) RootNamespaces(ctx context.Context, opts forge.BrowseOptions) ([]forge.Namespace, error) {
	orgs, err := listAll(func(page *gh.ListOptions) ([]*gh.Organization, *gh.Response, error) {
		return f.client.Organizations.List(ctx, "", page)
	})
	if err != nil {
		return nil, err
	}

	namespaces := f.namespaces(ctx, orgs, f.decoratingAs(ctx, opts))

	// The personal namespace is prepended rather than appended: it is where a
	// solo account's repositories are, and burying it under a list of
	// organisations would hide the only row half of them have.
	if personal, ok := f.personalNamespace(ctx); ok {
		return append([]forge.Namespace{personal}, namespaces...), nil
	}
	return namespaces, nil
}

// personalNamespace is the signed-in user as a namespace.
//
// **Its ID is the empty string, and that is the interface's own word for it**:
// forge.NewRepository.NamespaceID documents "empty means the user's own
// namespace", and go-github's Repositories.Create takes exactly that for the
// `org` argument. Passing the login instead would 404 — GitHub refuses to treat
// a user as an organisation, which is the same distinction this row exists to
// carry.
//
// A user that cannot be read yields no row rather than an error: the
// organisations are still worth showing, and CurrentUser is memoised so this
// costs nothing after the first call.
func (f *Forge) personalNamespace(ctx context.Context) (forge.Namespace, bool) {
	user, err := f.CurrentUser(ctx)
	if err != nil {
		return forge.Namespace{}, false
	}
	return forge.Namespace{
		ID:     "",
		Path:   user.Username,
		Name:   firstNonEmpty(user.Name, user.Username),
		WebURL: f.webBase() + "/" + user.Username,
		// Owner without asking: it is the account the token belongs to.
		Role: "Owner",
	}, true
}

// Children lists an organisation's repositories.
//
// Never namespaces: an organisation does not hold another one, which is what a
// MaxNamespaceDepth of 1 declares. The empty slice is the answer, not a missing
// feature.
func (f *Forge) Children(ctx context.Context, namespaceID string, opts forge.BrowseOptions) (forge.Children, error) {
	repos, err := f.repositoriesOf(ctx, namespaceID)
	if err != nil {
		return forge.Children{}, err
	}

	// GitHub has no "exclude archived" filter, so they are dropped after the
	// fact. It costs nothing extra: they were in the page either way.
	if !opts.IncludeArchived {
		kept := repos[:0]
		for _, repo := range repos {
			if !repo.GetArchived() {
				kept = append(kept, repo)
			}
		}
		repos = kept
	}

	return forge.Children{Repositories: f.repositories(ctx, repos, f.decoratingAs(ctx, opts))}, nil
}

// repositoriesOf lists what a namespace holds, every page.
//
// The empty namespace is the signed-in user's own, and it takes a different
// endpoint: /user/repos rather than /orgs/{org}/repos. `Affiliation: owner`
// keeps it to what the user owns — without it the list also carries every
// repository they collaborate on, which belongs under whoever owns it and would
// appear twice in a tree that shows both.
func (f *Forge) repositoriesOf(ctx context.Context, namespaceID string) ([]*gh.Repository, error) {
	if namespaceID == "" {
		opts := &gh.RepositoryListByAuthenticatedUserOptions{Affiliation: "owner"}
		return listAll(func(page *gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
			opts.ListOptions = *page
			return f.client.Repositories.ListByAuthenticatedUser(ctx, opts)
		})
	}

	opts := &gh.RepositoryListByOrgOptions{Type: "all"}
	return listAll(func(page *gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
		opts.ListOptions = *page
		return f.client.Repositories.ListByOrg(ctx, namespaceID, opts)
	})
}

// decoratingAs resolves the caller's login once per listing, or "" when no
// decoration was asked for. Once per listing, not once per row — the same cost
// argument as the GitLab backend's.
func (f *Forge) decoratingAs(ctx context.Context, opts forge.BrowseOptions) string {
	if !opts.Decorated {
		return ""
	}
	user, err := f.CurrentUser(ctx)
	if err != nil {
		return ""
	}
	return user.Username
}

// namespaces converts organisations.
func (f *Forge) namespaces(ctx context.Context, orgs []*gh.Organization, login string) []forge.Namespace {
	out := make([]forge.Namespace, 0, len(orgs))
	for _, org := range orgs {
		created := org.GetCreatedAt().Time
		ns := forge.Namespace{
			ID:     org.GetLogin(),
			Path:   org.GetLogin(),
			Name:   firstNonEmpty(org.GetName(), org.GetLogin()),
			WebURL: f.webBase() + "/" + org.GetLogin(),
			// An organisation has no visibility of its own on GitHub — its
			// repositories do. Left empty rather than invented.
			CreatedAt: &created,
		}
		if login != "" {
			ns.Role = f.orgRole(ctx, org.GetLogin(), login)
		}
		out = append(out, ns)
	}
	return out
}

// repositories converts repositories, decorating each with the caller's role
// and the last workflow run when asked.
//
// The decoration is two extra requests per repository here too, and the clone's
// walk asks for none of it.
func (f *Forge) repositories(ctx context.Context, repos []*gh.Repository, login string) []forge.Repository {
	out := make([]forge.Repository, 0, len(repos))
	for _, repo := range repos {
		created, pushed := repo.GetCreatedAt().Time, repo.GetPushedAt().Time
		full := repo.GetFullName()

		converted := forge.Repository{
			ID:             full,
			Path:           full,
			Name:           repo.GetName(),
			Visibility:     visibilityOf(repo),
			CreatedAt:      &created,
			LastActivityAt: &pushed,
			WebURL:         repo.GetHTMLURL(),
			// GitHub deletes at once, so a repository is never *scheduled* for
			// deletion. Always false, and Shape.PermanentDelete says why.
			DeletionScheduled: false,
		}
		if login != "" {
			converted.Role = roleName(repo.GetPermissions())
			converted.CIStatus = f.lastWorkflowStatus(ctx, repo.GetOwner().GetLogin(), repo.GetName())
		}
		out = append(out, converted)
	}
	return out
}

// visibilityOf reads a repository's visibility.
//
// Visibility is the field Enterprise populates with `internal`; Private is the
// older boolean github.com always sets. Reading the first and falling back to
// the second is what keeps an Enterprise `internal` from being reported as
// `private` — a visibility this platform's Shape does not offer for creation,
// but which an Enterprise instance really has.
func visibilityOf(repo *gh.Repository) string {
	if v := repo.GetVisibility(); v != "" {
		return v
	}
	if repo.GetPrivate() {
		return "private"
	}
	return "public"
}

// roleName turns GitHub's permission flags into the word
// forge.Repository.Role promises.
//
// They arrive as a set of booleans rather than as a level, so the mapping reads
// most-privileged first. Nothing means the permissions were not returned, which
// is not the same as "no access" — hence the empty string, as on GitLab.
func roleName(perms map[string]bool) string {
	switch {
	case perms["admin"]:
		return "Owner"
	case perms["maintain"]:
		return "Maintainer"
	case perms["push"]:
		return "Developer"
	case perms["triage"]:
		return "Reporter"
	case perms["pull"]:
		return "Guest"
	default:
		return ""
	}
}

// orgRole is the caller's humanised role in an organisation.
//
// Two words, not five: GitHub's organisation membership is admin or member, and
// inventing the three levels in between to match GitLab's would be reporting a
// distinction the platform does not make.
//
// A membership that cannot be read is not an error — the explorer would show
// nothing rather than a column short, which is the wrong trade (§3.6 step 2).
func (f *Forge) orgRole(ctx context.Context, org, login string) string {
	membership, _, err := f.client.Organizations.GetOrgMembership(ctx, login, org)
	if err != nil {
		return ""
	}
	if membership.GetRole() == "admin" {
		return "Owner"
	}
	return "Member"
}

// lastWorkflowStatus is the most recent Actions run, translated into the
// vocabulary forge.CIStatus* declares.
//
// A run still in flight has no conclusion, so its *status* is used instead —
// otherwise a repository whose build is running would read as having no CI at
// all. The two are different alphabets, which is why ciStatusOf takes both and
// not one string.
//
// It was called lastWorkflowConclusion and claimed to return "the same words
// the GitLab backend reports". It never did (D66): only `success` and `skipped`
// happen to be spelled the same, so `failure` reached the CI column as raw text
// truncated to `failu…` and coloured orange by the view's default branch — a
// broken build rendered as a warning, which is the one thing that column exists
// to prevent.
func (f *Forge) lastWorkflowStatus(ctx context.Context, owner, repo string) string {
	runs, _, err := f.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo,
		&gh.ListWorkflowRunsOptions{ListOptions: gh.ListOptions{PerPage: 1, Page: 1}})
	if err != nil || runs == nil || len(runs.WorkflowRuns) == 0 {
		return ""
	}

	run := runs.WorkflowRuns[0]
	return ciStatusOf(run.GetConclusion(), run.GetStatus())
}

// ciStatusOf translates one Actions run onto forge's vocabulary.
//
// The conclusion is read first and the status only when there is none: a
// completed run carries both, and its `completed` status says nothing a reader
// wants — which is precisely why `completed` appears in neither table below.
//
// **An unrecognised value returns the empty string**, not itself. That is what
// forge.Repository.CIStatus documents for a value the backend has no equivalent
// for, and it is the difference between an empty cell and six characters of
// GitHub's internal spelling. Returning the raw word was the defect.
func ciStatusOf(conclusion, status string) string {
	if conclusion != "" {
		return ciConclusions[conclusion]
	}
	return ciRunStates[status]
}

// ciConclusions maps a finished run. Four of GitHub's nine outcomes have no
// counterpart and are folded rather than dropped, each on its own argument:
//
//   - `timed_out` and `startup_failure` are **failures**. GitLab has no separate
//     status for either — a job it kills on timeout ends `failed` — so folding
//     them is reporting what GitLab would have reported, not losing detail.
//   - `action_required` is a run waiting for someone to approve it, which is
//     exactly what GitLab calls `manual`.
//   - `neutral` and `stale` are the grey outcomes: ran without a verdict, and
//     superseded before finishing. GitHub renders both the way it renders
//     `skipped`, and `skipped` is the only grey this vocabulary has.
var ciConclusions = map[string]string{
	"success":         forge.CIStatusSuccess,
	"failure":         forge.CIStatusFailed,
	"timed_out":       forge.CIStatusFailed,
	"startup_failure": forge.CIStatusFailed,
	"cancelled":       forge.CIStatusCanceled, // GitHub spells it with two l's
	"action_required": forge.CIStatusManual,
	"skipped":         forge.CIStatusSkipped,
	"neutral":         forge.CIStatusSkipped,
	"stale":           forge.CIStatusSkipped,
}

// ciRunStates maps a run that has not finished. GitLab separates `pending` from
// `created`, `preparing` and `waiting_for_resource`; GitHub's four pre-run
// states all mean the same thing to a reader — it has not started — so they
// collapse onto the one word the CI column already draws a clock for.
var ciRunStates = map[string]string{
	"in_progress": forge.CIStatusRunning,
	"queued":      forge.CIStatusPending,
	"requested":   forge.CIStatusPending,
	"waiting":     forge.CIStatusPending,
	"pending":     forge.CIStatusPending,
}

// splitPath cuts an owner/repo identifier.
//
// GitHub addresses a repository by its two halves in every write call, and this
// is the one place that knows the identifier has halves at all — outside this
// package it is opaque.
func splitPath(id string) (owner, repo string, ok bool) {
	owner, repo, ok = strings.Cut(id, "/")
	return owner, repo, ok && owner != "" && repo != ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
