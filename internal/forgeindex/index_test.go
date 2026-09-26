package forgeindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/forge"
)

// treeForge is an in-memory forge: a map from namespace ID to what it holds.
// Only the two listing calls are implemented; the embedded interface panics on
// anything else, which is what a walk must never call.
type treeForge struct {
	forge.Forge
	roots    []forge.Namespace
	children map[string]forge.Children
	fail     map[string]bool
	calls    atomic.Int32
	decor    atomic.Bool
}

func (f *treeForge) RootNamespaces(_ context.Context, opts forge.BrowseOptions) ([]forge.Namespace, error) {
	if opts.Decorated {
		f.decor.Store(true)
	}
	return f.roots, nil
}

func (f *treeForge) Children(ctx context.Context, id string, opts forge.BrowseOptions) (forge.Children, error) {
	f.calls.Add(1)
	if opts.Decorated {
		f.decor.Store(true)
	}
	if err := ctx.Err(); err != nil {
		return forge.Children{}, err
	}
	if f.fail[id] {
		return forge.Children{}, errors.New("403")
	}
	return f.children[id], nil
}

// threeLevels is acme → platform → infra, with a repository at each level.
func threeLevels() *treeForge {
	return &treeForge{
		roots: []forge.Namespace{{ID: "1", Path: "acme", Name: "Acme"}},
		children: map[string]forge.Children{
			"1": {
				Namespaces:   []forge.Namespace{{ID: "2", Path: "acme/platform", Name: "Platform"}},
				Repositories: []forge.Repository{{ID: "10", Path: "acme/www", Name: "www"}},
			},
			"2": {
				Namespaces: []forge.Namespace{{ID: "3", Path: "acme/platform/infra", Name: "Infra"}},
				Repositories: []forge.Repository{
					{ID: "11", Path: "acme/platform/api", Name: "api"},
					{ID: "12", Path: "acme/platform/web", Name: "web"},
				},
			},
			"3": {
				Repositories: []forge.Repository{{ID: "13", Path: "acme/platform/infra/terraform-modules", Name: "terraform-modules"}},
			},
		},
	}
}

func paths(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func TestBuildWalksEveryLevelUndecorated(t *testing.T) {
	f := threeLevels()
	ix, err := Build(context.Background(), f, "gitlab.example.com", "alice", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := ix.Len(); got != 7 {
		t.Errorf("Len() = %d, want 7 (3 namespaces + 4 repositories)", got)
	}
	if f.decor.Load() {
		t.Error("the walk asked for decoration — two requests per repository on GitLab")
	}
	if !ix.Matches("gitlab.example.com", "alice") || ix.Matches("gitlab.example.com", "bob") {
		t.Error("Matches does not tell the owner apart")
	}

	level, known := ix.Children("acme/platform")
	if !known {
		t.Fatal("acme/platform should be a known level")
	}
	// The forge's order: namespaces, then repositories.
	want := []string{"acme/platform/infra", "acme/platform/api", "acme/platform/web"}
	if got := paths(level); !reflect.DeepEqual(got, want) {
		t.Errorf("Children(acme/platform) = %v, want %v", got, want)
	}
	if e, _ := ix.Lookup("acme/platform/infra/terraform-modules"); e.Kind != KindRepository || e.Parent != "acme/platform/infra" {
		t.Errorf("deep repository = %+v", e)
	}
}

func TestBuildKeepsAFailedNamespaceAsUnknownNotEmpty(t *testing.T) {
	f := threeLevels()
	f.fail = map[string]bool{"2": true}
	ix, err := Build(context.Background(), f, "h", "u", time.Now())
	if err != nil {
		t.Fatalf("one unreadable namespace must not fail the whole walk: %v", err)
	}
	if ix.Skipped() != 1 {
		t.Errorf("Skipped() = %d, want 1", ix.Skipped())
	}
	if _, ok := ix.Lookup("acme/platform"); !ok {
		t.Error("the namespace itself was listed by its parent and must stay")
	}
	if _, known := ix.Children("acme/platform"); known {
		t.Error("the content of a namespace whose listing failed is unknown, not empty")
	}
}

func TestBuildStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, threeLevels(), "h", "u", time.Now()); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAncestorsRunFromTheRootDown(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Now())
	chain, ok := ix.Ancestors("acme/platform/infra/terraform-modules")
	if !ok {
		t.Fatal("chain should reach a root")
	}
	want := []string{"acme", "acme/platform", "acme/platform/infra"}
	if got := paths(chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Ancestors = %v, want %v", got, want)
	}
	if chain, ok := ix.Ancestors("acme"); !ok || len(chain) != 0 {
		t.Errorf("a root has no ancestors, got %v %v", paths(chain), ok)
	}
}

func TestWithoutTakesTheSubtree(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Now())
	next := ix.Without("acme/platform")
	if got := next.Len(); got != 2 {
		t.Errorf("Len() after removing acme/platform = %d, want 2 (acme, acme/www)", got)
	}
	if ix.Len() != 7 {
		t.Error("Without changed the receiver — an index is a value")
	}
}

func TestWithAppendsToItsLevel(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Now())
	next := ix.With(Entry{ID: "99", Path: "acme/new", Name: "new", Parent: "acme", Kind: KindRepository})
	level, _ := next.Children("acme")
	if got := paths(level); got[len(got)-1] != "acme/new" {
		t.Errorf("a created entry goes last in its level, got %v", got)
	}
	if _, ok := ix.Lookup("acme/new"); ok {
		t.Error("With changed the receiver")
	}
}

func TestReplaceLevelDropsWhatTheForgeNoLongerLists(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Now())
	// platform/infra is gone, api stays, a new repository appeared.
	fresh := []Entry{
		{ID: "11", Path: "acme/platform/api", Name: "api", Kind: KindRepository},
		{ID: "14", Path: "acme/platform/docs", Name: "docs", Kind: KindRepository},
	}
	next := ix.ReplaceLevel("acme/platform", fresh)
	level, _ := next.Children("acme/platform")
	if got, want := paths(level), []string{"acme/platform/api", "acme/platform/docs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("level = %v, want %v", got, want)
	}
	if _, ok := next.Lookup("acme/platform/infra/terraform-modules"); ok {
		t.Error("a namespace that went takes its content with it")
	}
	if _, ok := next.Lookup("acme/www"); !ok {
		t.Error("another level was touched")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Unix(1700000000, 0).UTC())
	path := filepath.Join(t.TempDir(), "sub", "ctx.json")
	if err := Save(path, ix); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != ix.Len() || !got.Matches("h", "u") || !got.BuiltAt.Equal(ix.BuiltAt) {
		t.Errorf("round trip lost something: %d entries, %s/%s", got.Len(), got.Host, got.User)
	}
	if level, known := got.Children("acme/platform"); !known || len(level) != 3 {
		t.Error("the lookups were not rebuilt after the decode")
	}
}

func TestLoadAnswersNothingForAMissingOrForeignFile(t *testing.T) {
	dir := t.TempDir()
	if ix, err := Load(filepath.Join(dir, "absent.json")); ix != nil || err != nil {
		t.Errorf("missing file = %v, %v; want nil, nil", ix, err)
	}
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"version":0,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ix, err := Load(old); ix != nil || err != nil {
		t.Errorf("another version = %v, %v; want nil, nil", ix, err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("a corrupt file should be reported")
	}
}

func TestPathStaysInsideTheCacheDirectory(t *testing.T) {
	if got := fileName("../../etc/passwd"); filepath.Base(got) != got {
		t.Errorf("fileName escaped: %q", got)
	}
}

// Re-reading a level the forge left as it was is nearly every read, and must
// not cost the caller a rewrite: the receiver comes back itself.
func TestAnUnchangedLevelOrAnAbsentPathChangesNothing(t *testing.T) {
	ix, _ := Build(context.Background(), threeLevels(), "h", "u", time.Now())
	same, _ := ix.Children("acme/platform")
	if next := ix.ReplaceLevel("acme/platform", same); next != ix {
		t.Error("an identical level produced a new index")
	}
	if next := ix.Without("acme/nowhere"); next != ix {
		t.Error("removing an absent path produced a new index")
	}
	renamed := append([]Entry(nil), same...)
	renamed[0].Name = "renamed"
	if next := ix.ReplaceLevel("acme/platform", renamed); next == ix {
		t.Error("a changed level was taken for the same one")
	}
}
