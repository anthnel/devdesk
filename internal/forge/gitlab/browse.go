package gitlab

import (
	"context"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/forge"
)

// perPage is what every list endpoint asks for. GitLab caps it at 100.
const perPage = 100

// listAll walks every page of a list endpoint and returns the lot (D34).
//
// Each caller used to pass `Page: 1` and keep only what came back, so a group
// with more than perPage subgroups or projects was silently cut: the explorer
// showed fewer than it had and a recursive clone skipped repositories without
// saying so. The loop lives here rather than at each call site because four
// copies of it is how one of them would end up wrong.
//
// opts is the caller's embedded ListOptions, mutated between calls, so the
// fetch closure sees the new page without the caller rebuilding its options.
func listAll[T any](opts *gitlabclient.ListOptions, fetch func() ([]T, *gitlabclient.Response, error)) ([]T, error) {
	opts.PerPage = perPage
	opts.Page = 1

	var all []T
	for {
		page, resp, err := fetch()
		if err != nil {
			return nil, err
		}
		all = append(all, page...)

		// NextPage is 0 on the last page. Comparing against the page just
		// fetched rather than against 0 alone also stops a server that keeps
		// pointing back at it, which would otherwise spin for ever — and it is
		// a bound that cannot cut a legitimate response short, which a page
		// limit would be.
		if resp == nil || resp.NextPage <= opts.Page {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// RootNamespaces lists the top-level groups, every page.
func (f *Forge) RootNamespaces(ctx context.Context, opts forge.BrowseOptions) ([]forge.Namespace, error) {
	listOpts := &gitlabclient.ListGroupsOptions{TopLevelOnly: gitlabclient.Ptr(true)}
	groups, err := listAll(&listOpts.ListOptions, func() ([]*gitlabclient.Group, *gitlabclient.Response, error) {
		return f.client.Groups.ListGroups(listOpts, gitlabclient.WithContext(ctx))
	})
	if err != nil {
		return nil, err
	}
	return f.namespaces(ctx, groups, f.decoratingAs(ctx, opts)), nil
}

// decoratingAs resolves the current user once per listing, or reports 0 when
// the listing asks for no decoration.
//
// Once per listing, not once per row: the role lookups need the caller's ID,
// and asking for it inside the loop would add one request per group — which is
// how a decoration that already costs two calls per repository would have come
// to cost three.
func (f *Forge) decoratingAs(ctx context.Context, opts forge.BrowseOptions) int64 {
	if !opts.Decorated {
		return 0
	}
	user, err := f.CurrentUser(ctx)
	if err != nil {
		return 0
	}
	uid, err := numericID(user.ID)
	if err != nil {
		return 0
	}
	return uid
}

// Children lists a group's direct subgroups and its projects, every page of
// each.
//
// Direct, not recursive: the explorer drills one level at a time and the clone
// walks levels itself.
func (f *Forge) Children(ctx context.Context, namespaceID string, opts forge.BrowseOptions) (forge.Children, error) {
	subOpts := &gitlabclient.ListSubGroupsOptions{}
	subgroups, err := listAll(&subOpts.ListOptions, func() ([]*gitlabclient.Group, *gitlabclient.Response, error) {
		return f.client.Groups.ListSubGroups(namespaceID, subOpts, gitlabclient.WithContext(ctx))
	})
	if err != nil {
		return forge.Children{}, err
	}

	projOpts := &gitlabclient.ListGroupProjectsOptions{}
	if !opts.IncludeArchived {
		projOpts.Archived = gitlabclient.Ptr(false)
	}
	projects, err := listAll(&projOpts.ListOptions, func() ([]*gitlabclient.Project, *gitlabclient.Response, error) {
		return f.client.Groups.ListGroupProjects(namespaceID, projOpts, gitlabclient.WithContext(ctx))
	})
	if err != nil {
		return forge.Children{}, err
	}

	uid := f.decoratingAs(ctx, opts)
	return forge.Children{
		Namespaces:   f.namespaces(ctx, subgroups, uid),
		Repositories: f.repositories(ctx, projects, uid),
	}, nil
}

// namespaces converts groups, decorating each with the caller's role when uid
// is non-zero.
func (f *Forge) namespaces(ctx context.Context, groups []*gitlabclient.Group, uid int64) []forge.Namespace {
	out := make([]forge.Namespace, 0, len(groups))
	for _, g := range groups {
		ns := forge.Namespace{
			ID:         namespaceID(g.ID),
			Path:       g.FullPath,
			Name:       g.Name,
			Visibility: string(g.Visibility),
			CreatedAt:  g.CreatedAt,
			WebURL:     g.WebURL,
		}
		if uid != 0 {
			ns.Role = f.groupRole(ctx, g.ID, uid)
		}
		out = append(out, ns)
	}
	return out
}

// repositories converts projects, decorating each with the caller's role and
// the last pipeline status when asked.
//
// The decoration is two extra requests **per project**, which is why it is an
// option and not a default: a two-hundred-repository group costs 400 calls for
// a CI badge and a role that a clone never reads.
func (f *Forge) repositories(ctx context.Context, projects []*gitlabclient.Project, uid int64) []forge.Repository {
	out := make([]forge.Repository, 0, len(projects))
	for _, p := range projects {
		repo := forge.Repository{
			ID:                namespaceID(int64(p.ID)),
			Path:              p.PathWithNamespace,
			Name:              p.Name,
			Visibility:        string(p.Visibility),
			CreatedAt:         p.CreatedAt,
			LastActivityAt:    p.LastActivityAt,
			WebURL:            p.WebURL,
			DeletionScheduled: p.MarkedForDeletionOn != nil,
		}
		if uid != 0 {
			repo.Role = f.projectRole(ctx, int64(p.ID), uid)
			repo.CIStatus = f.lastPipelineStatus(ctx, int64(p.ID))
		}
		out = append(out, repo)
	}
	return out
}

// groupRole is the caller's humanised role in a group, empty when it cannot be
// read. A decoration that fails is not worth failing the listing for: the
// explorer would show nothing rather than a column short.
func (f *Forge) groupRole(ctx context.Context, groupID, uid int64) string {
	member, _, err := f.client.GroupMembers.GetInheritedGroupMember(int(groupID), uid, gitlabclient.WithContext(ctx))
	if err != nil {
		return ""
	}
	return roleName(int(member.AccessLevel))
}

// projectRole is the caller's humanised role in a project.
func (f *Forge) projectRole(ctx context.Context, projectID, uid int64) string {
	member, _, err := f.client.ProjectMembers.GetInheritedProjectMember(int(projectID), uid, gitlabclient.WithContext(ctx))
	if err != nil {
		return ""
	}
	return roleName(int(member.AccessLevel))
}

// lastPipelineStatus is the most recent pipeline's status, empty when there is
// none or it cannot be read.
func (f *Forge) lastPipelineStatus(ctx context.Context, projectID int64) string {
	pipelines, _, err := f.client.Pipelines.ListProjectPipelines(projectID, &gitlabclient.ListProjectPipelinesOptions{
		ListOptions: gitlabclient.ListOptions{PerPage: 1, Page: 1},
	}, gitlabclient.WithContext(ctx))
	if err != nil || len(pipelines) == 0 {
		return ""
	}
	return pipelines[0].Status
}
