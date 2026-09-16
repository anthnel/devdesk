package forge

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// gitlabShape and githubShape are the two shapes §3.6 settled, written out so
// the tests below argue about the real difference rather than an invented one.
var (
	gitlabShape = Shape{
		Name:                        "gitlab",
		MaxNamespaceDepth:           0, // unbounded
		Visibilities:                []string{"private", "internal", "public"},
		PermanentDelete:             true,
		RestrictsVisibilityByParent: true,
	}
	githubShape = Shape{
		Name:              "github",
		MaxNamespaceDepth: 1, // an organisation never holds an organisation
		Visibilities:      []string{"private", "public"},
		PermanentDelete:   false,
		// An organisation is not itself public/private the way a GitLab group
		// is, so RestrictsVisibilityByParent stays at its zero value, false.
	}
)

func TestAnUnboundedForgeNestsAtAnyDepth(t *testing.T) {
	for _, depth := range []int{0, 1, 2, 7, 40} {
		if !gitlabShape.CanNestUnder(depth) {
			t.Errorf("CanNestUnder(%d) = false on an unbounded forge, want true", depth)
		}
	}
}

// TestGitHubTakesARootNamespaceAndRefusesANestedOne is the sentinel's whole
// point: MaxNamespaceDepth is 1, and a comparison written the obvious way
// (depth < max) would refuse the root too.
func TestGitHubTakesARootNamespaceAndRefusesANestedOne(t *testing.T) {
	if !githubShape.CanNestUnder(0) {
		t.Error("CanNestUnder(0) = false, want true — every forge takes a root namespace")
	}
	if githubShape.CanNestUnder(1) {
		t.Error("CanNestUnder(1) = true, want false — an organisation does not hold an organisation")
	}
}

func TestTheDefaultVisibilityIsTheMostPrivate(t *testing.T) {
	if got := gitlabShape.DefaultVisibility(); got != "private" {
		t.Errorf("DefaultVisibility() = %q, want %q", got, "private")
	}
	if got := githubShape.DefaultVisibility(); got != "private" {
		t.Errorf("DefaultVisibility() = %q, want %q", got, "private")
	}
	if got := (Shape{}).DefaultVisibility(); got != "" {
		t.Errorf("a backend declaring no visibility returned %q, want the empty string", got)
	}
}

// TestInternalIsAGitLabVisibilityOnly is the shape the creation form reads its
// choices from. On GitHub.com `internal` is refused by the server, which is
// exactly what a declared set stops the form offering.
func TestInternalIsAGitLabVisibilityOnly(t *testing.T) {
	if !gitlabShape.AllowsVisibility("internal") {
		t.Error("gitlab refuses `internal`, want it allowed")
	}
	if githubShape.AllowsVisibility("internal") {
		t.Error("github allows `internal`, want it refused")
	}
}

// TestAProjectCannotBeMoreOpenThanItsGroup is D71: GitLab refuses a subgroup or
// a project created more open than the group it goes into, so the form is
// narrowed to match before the round trip rather than after it fails.
func TestAProjectCannotBeMoreOpenThanItsGroup(t *testing.T) {
	tests := []struct {
		parentVisibility string
		want             []string
	}{
		{"private", []string{"private"}},
		{"internal", []string{"private", "internal"}},
		{"public", []string{"private", "internal", "public"}},
		// The root has no parent to narrow by.
		{"", []string{"private", "internal", "public"}},
	}
	for _, tt := range tests {
		t.Run(tt.parentVisibility, func(t *testing.T) {
			got := gitlabShape.VisibilitiesUnder(tt.parentVisibility)
			if !slices.Equal(got, tt.want) {
				t.Errorf("VisibilitiesUnder(%q) = %v, want %v", tt.parentVisibility, got, tt.want)
			}
		})
	}
}

// An unrecognised parent value narrows to nothing usable if trusted blindly.
// Falling back to the full list means the create call reaches the server and
// is refused there — a real answer rather than a menu that silently offers
// one option for a reason nobody can see.
func TestVisibilitiesUnderFallsBackOnAnUnrecognisedParentValue(t *testing.T) {
	got := gitlabShape.VisibilitiesUnder("classified")
	if !slices.Equal(got, gitlabShape.Visibilities) {
		t.Errorf("VisibilitiesUnder(%q) = %v, want the full list", "classified", got)
	}
}

// TestGitHubIgnoresTheParentVisibility is the other half of D71: an
// organisation has no visibility of its own for a repository to be narrowed
// against, so GitHub answers the same whatever parentVisibility is passed.
func TestGitHubIgnoresTheParentVisibility(t *testing.T) {
	for _, parent := range []string{"", "private", "public", "anything"} {
		if got := githubShape.VisibilitiesUnder(parent); !slices.Equal(got, githubShape.Visibilities) {
			t.Errorf("VisibilitiesUnder(%q) = %v, want the full list unnarrowed", parent, got)
		}
	}
}

// TestABackendDeclaringNoVisibilityAllowsNone pins the direction the zero value
// falls in. Allowing everything would let an unconfigured backend hand the
// forge a value it will refuse, and the error would name the server rather than
// the configuration.
func TestABackendDeclaringNoVisibilityAllowsNone(t *testing.T) {
	if (Shape{}).AllowsVisibility("private") {
		t.Error("a backend declaring no visibility allowed `private`, want it refused")
	}
}

// TestACounterIsNilUntilSomethingCountedIt is the D20 guard in the type: the
// zero value of DashboardStats says "nobody looked", not "there are none".
func TestACounterIsNilUntilSomethingCountedIt(t *testing.T) {
	var stats DashboardStats
	if stats.AssignedIssues != nil {
		t.Error("the zero DashboardStats reports a known issue count, want nil")
	}

	stats.AssignedIssues = Count(0)
	if stats.AssignedIssues == nil || *stats.AssignedIssues != 0 {
		t.Errorf("Count(0) = %v, want a pointer to 0 — a counted zero is an answer", stats.AssignedIssues)
	}
}

// stubForge is here to answer one question: is the interface implementable and
// complete? An interface with no implementation compiles whatever it declares,
// which is the way a step that only defines one goes wrong.
//
// It is deliberately in the test file. Step 2 is what gains callers that want a
// double, and a helper package written before its consumers would be guessing
// at what they need.
type stubForge struct {
	shape Shape
	user  User
}

var _ Forge = (*stubForge)(nil)

var errStub = errors.New("stub")

func (f *stubForge) Shape() Shape { return f.shape }

func (f *stubForge) CurrentUser(context.Context) (User, error) { return f.user, nil }

func (f *stubForge) RootNamespaces(context.Context, BrowseOptions) ([]Namespace, error) {
	return []Namespace{{ID: "1", Path: "acme", Name: "Acme"}}, nil
}

func (f *stubForge) Children(context.Context, string, BrowseOptions) (Children, error) {
	return Children{}, nil
}

func (f *stubForge) CreateNamespace(_ context.Context, spec NewNamespace) (Namespace, error) {
	if !f.shape.AllowsVisibility(spec.Visibility) {
		return Namespace{}, errStub
	}
	return Namespace{ID: "2", Path: spec.Slug, Name: spec.Name}, nil
}

func (f *stubForge) CreateRepository(_ context.Context, spec NewRepository) (Repository, error) {
	return Repository{ID: "3", Path: spec.Slug, Name: spec.Name}, nil
}

func (f *stubForge) InitialCommit(context.Context, string, []FileChange) error { return nil }

func (f *stubForge) DeleteNamespace(_ context.Context, _ Namespace, permanently bool) error {
	if permanently && !f.shape.PermanentDelete {
		return errStub
	}
	return nil
}

func (f *stubForge) DeleteRepository(_ context.Context, _ Repository, permanently bool) error {
	if permanently && !f.shape.PermanentDelete {
		return errStub
	}
	return nil
}

func (f *stubForge) DashboardStats(context.Context, User) (DashboardStats, error) {
	return DashboardStats{AssignedIssues: Count(2)}, nil
}

func (f *stubForge) CloneURL(repo Repository, method CloneMethod) string {
	if method == CloneSSH {
		return "git@example.test:" + repo.Path + ".git"
	}
	return "https://example.test/" + repo.Path + ".git"
}

func (f *stubForge) MergedCIConfig(context.Context, string, string) (string, error) {
	return "stages: [build]\n", nil
}
func (f *stubForge) ChangeRequestsURL() string { return "https://example.test/mrs" }
func (f *stubForge) AssignedIssuesURL(u User) string {
	return "https://example.test/issues/" + u.Username
}

// TestABackendRefusesAPermanentDeleteItCannotDo is what the Shape flag is for.
// Deleting normally and reporting success would tell the user their data is
// gone when the forge has only scheduled it — or, on GitHub, that a grace
// period exists when it does not.
func TestABackendRefusesAPermanentDeleteItCannotDo(t *testing.T) {
	github := &stubForge{shape: githubShape}
	if err := github.DeleteNamespace(context.Background(), Namespace{ID: "1"}, true); err == nil {
		t.Error("a permanent delete succeeded on a forge that cannot do one, want an error")
	}
	if err := github.DeleteNamespace(context.Background(), Namespace{ID: "1"}, false); err != nil {
		t.Errorf("an ordinary delete failed: %v", err)
	}

	gitlab := &stubForge{shape: gitlabShape}
	if err := gitlab.DeleteNamespace(context.Background(), Namespace{ID: "1"}, true); err != nil {
		t.Errorf("a permanent delete failed on a forge that can do one: %v", err)
	}
}
