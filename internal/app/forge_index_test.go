package app

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// indexForge is an in-memory forge with one group holding one repository. The
// embedded interface panics on anything a walk must never call.
type indexForge struct {
	forge.Forge
}

func (indexForge) RootNamespaces(context.Context, forge.BrowseOptions) ([]forge.Namespace, error) {
	return []forge.Namespace{{ID: "1", Path: "acme", Name: "Acme"}}, nil
}

func (indexForge) Children(_ context.Context, id string, _ forge.BrowseOptions) (forge.Children, error) {
	if id == "1" {
		return forge.Children{Repositories: []forge.Repository{{ID: "2", Path: "acme/api", Name: "api"}}}, nil
	}
	return forge.Children{}, nil
}

// pumpIndex feeds msg to the router and then every forge-index message and job
// start its commands produce, until none is left. Anything else — the spinner
// chain above all — is dropped, so the loop ends.
func pumpIndex(t *testing.T, a *App, msg tea.Msg) {
	t.Helper()
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		_, cmd := a.Update(next)
		for _, out := range testutil.Msgs(cmd) {
			switch out.(type) {
			case forgeIndexLoadedMsg, forgeIndexStartedMsg, forgeIndexBuiltMsg, jobs.StartMsg:
				queue = append(queue, out)
			}
		}
	}
}

func indexRouter(t *testing.T) *App {
	t.Helper()
	testutil.FastTimers(t, &jobSpinnerInterval)
	a := router(t, &fakeView{})
	a.config.Forge.URL = "https://gitlab.example.com"
	a.currentContext = t.Name()
	return a
}

func TestASessionWalksTheForgeAsAJobAndKeepsTheIndex(t *testing.T) {
	a := indexRouter(t)

	pumpIndex(t, a, ForgeAutoLoginMsg{Forge: indexForge{}, User: forge.User{ID: "7", Username: "anthnel"}})

	ix := a.sharedState.ForgeIndex
	if ix.Len() != 2 {
		t.Fatalf("index holds %d entries, want acme and acme/api", ix.Len())
	}
	runs := a.jobs.Snapshot()
	if len(runs) != 1 || runs[0].Kind != jobs.KindIndex || runs[0].State() != jobs.RunDone {
		t.Errorf("runs = %+v, want one finished index run", runs)
	}

	// The walk was written for the next session, and only that account reads it.
	saved, err := forgeindex.Load(forgeindex.Path(a.currentContext))
	if err != nil || saved == nil {
		t.Fatalf("nothing on disk after the walk: %v", err)
	}
	if !saved.Matches("https://gitlab.example.com", "7") {
		t.Errorf("file written for %s/%s", saved.Host, saved.User)
	}
}

// The next session shows the file before its own walk lands, and an index
// written for someone else is not shown at all.
func TestTheFileIsTheHeadStartOnlyForItsOwner(t *testing.T) {
	a := indexRouter(t)
	path := forgeindex.Path(a.currentContext)
	old := forgeindex.New("https://gitlab.example.com", "7", time.Now(),
		[]forgeindex.Entry{{ID: "9", Path: "old", Name: "old", Kind: forgeindex.KindNamespace}}, nil)
	if err := forgeindex.Save(path, old); err != nil {
		t.Fatal(err)
	}

	a.sharedState.CurrentUser = forge.User{ID: "7"}
	a.sharedState.Forge = indexForge{}
	loaded := testutil.Msgs(a.loadForgeIndex())[0]
	a.Update(loaded)
	if _, ok := a.sharedState.ForgeIndex.Lookup("old"); !ok {
		t.Error("the file of the same account was not shown")
	}

	a.stopForgeIndex()
	a.sharedState.CurrentUser = forge.User{ID: "8"}
	a.Update(testutil.Msgs(a.loadForgeIndex())[0])
	if a.sharedState.ForgeIndex != nil {
		t.Error("another account's index was shown")
	}
}

// An answer for a session that has since closed is dropped, and the file does
// not overwrite a walk that already landed.
func TestStaleAnswersAreDropped(t *testing.T) {
	a := indexRouter(t)
	fresh := forgeindex.New("h", "u", time.Now(), nil, nil)
	gen := a.forgeIndexGen

	a.stopForgeIndex()
	a.Update(forgeIndexBuiltMsg{gen: gen, index: fresh})
	if a.sharedState.ForgeIndex != nil {
		t.Error("a walk from a closed session was installed")
	}

	a.forgeIndexFresh = true
	a.Update(forgeIndexLoadedMsg{gen: a.forgeIndexGen, index: fresh})
	if a.sharedState.ForgeIndex != nil {
		t.Error("the file replaced a walk that had already landed")
	}
}

func TestLogoutDropsTheIndex(t *testing.T) {
	a := indexRouter(t)
	pumpIndex(t, a, ForgeAutoLoginMsg{Forge: indexForge{}, User: forge.User{ID: "7"}})

	a.clearAuthenticated()

	if a.sharedState.ForgeIndex != nil {
		t.Error("the index outlived the session")
	}
}

func TestAViewEditIsAppliedToTheCurrentIndex(t *testing.T) {
	a := indexRouter(t)
	pumpIndex(t, a, ForgeAutoLoginMsg{Forge: indexForge{}, User: forge.User{ID: "7"}})

	a.Update(shared.ForgeIndexEditMsg{Edit: func(ix *forgeindex.Index) *forgeindex.Index {
		return ix.Without("acme/api")
	}})

	if _, ok := a.sharedState.ForgeIndex.Lookup("acme/api"); ok {
		t.Error("the edit was not applied")
	}
}

// ctrl+r in the explorer walks again, but not while a walk is already going.
func TestARefreshDoesNotStartASecondWalk(t *testing.T) {
	a := indexRouter(t)
	a.sharedState.Forge = indexForge{}
	a.forgeIndexCancel = func() {}

	if _, cmd := a.Update(shared.ForgeIndexRefreshMsg{}); cmd != nil {
		t.Error("a second walk was started beside the one in flight")
	}
}

// An edit made while a walk is out is replayed on the walk's answer, which was
// read before it: a delete confirmed meanwhile stays deleted.
func TestAnEditDuringAWalkSurvivesItsAnswer(t *testing.T) {
	a := indexRouter(t)
	a.forgeIndexCancel = func() {}
	built := forgeindex.New("h", "u", time.Now(), []forgeindex.Entry{
		{ID: "1", Path: "acme", Name: "acme", Kind: forgeindex.KindNamespace},
		{ID: "2", Path: "acme/api", Name: "api", Parent: "acme", Kind: forgeindex.KindRepository},
	}, nil)

	a.Update(shared.ForgeIndexEditMsg{Edit: func(ix *forgeindex.Index) *forgeindex.Index {
		return ix.Without("acme/api")
	}})
	a.Update(forgeIndexBuiltMsg{gen: a.forgeIndexGen, index: built})

	if _, ok := a.sharedState.ForgeIndex.Lookup("acme/api"); ok {
		t.Error("the walk brought back an entry deleted while it ran")
	}
	if len(a.forgeIndexPending) != 0 {
		t.Error("the replayed edits were kept for the next walk")
	}
}

// An edit that returns the index it was given writes nothing and tells no view.
func TestAnEditThatChangesNothingCostsNothing(t *testing.T) {
	a := indexRouter(t)
	a.sharedState.ForgeIndex = forgeindex.New("h", "u", time.Now(), nil, nil)

	_, cmd := a.Update(shared.ForgeIndexEditMsg{Edit: func(ix *forgeindex.Index) *forgeindex.Index { return ix }})
	if cmd != nil {
		t.Error("an unchanged index was written and broadcast")
	}
}
