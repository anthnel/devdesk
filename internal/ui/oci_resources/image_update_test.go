package ociresources

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imagepull"
	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// engineStub replaces the four engine calls an update makes, and records them.
type engineStub struct {
	users     []string
	pullErr   error
	newID     string
	removeErr error
	calls     []string
}

func stubEngine(t *testing.T, e *engineStub) {
	t.Helper()
	prev := [4]any{containersUsingImage, pullImageContext, imageIDOf, removeImage}
	containersUsingImage = func(id string) ([]string, error) { e.calls = append(e.calls, "ps "+id); return e.users, nil }
	pullImageContext = func(_ context.Context, ref string) error { e.calls = append(e.calls, "pull "+ref); return e.pullErr }
	imageIDOf = func(ref string) (string, error) { return e.newID, nil }
	removeImage = func(id string, force bool) error {
		e.calls = append(e.calls, "rmi "+id)
		if force {
			t.Error("an update forced a removal")
		}
		return e.removeErr
	}
	t.Cleanup(func() {
		containersUsingImage = prev[0].(func(string) ([]string, error))
		pullImageContext = prev[1].(func(context.Context, string) error)
		imageIDOf = prev[2].(func(string) (string, error))
		removeImage = prev[3].(func(string, bool) error)
	})
}

// onImage lists one image with the given registry answer, cursor on it.
func onImage(t *testing.T, img docker.Image, facts imageupdate.Facts) Model {
	t.Helper()
	return feed(t, newTestModel(t),
		ImagesListMsg{Images: []docker.Image{img}},
		ImageUpdatesCheckedMsg{Facts: map[string]imageupdate.Facts{img.Name(): facts}},
	)
}

var alpineOld = docker.Image{ID: "aaaaaaaaaaaa", Repository: "alpine", Tag: "latest", RepoDigests: []string{"alpine@sha256:old"}}

func TestGIsGreyedWithItsReasonWhenThereIsNothingToUpdate(t *testing.T) {
	now := time.Now()
	used := alpineOld
	used.Containers = 2
	for name, tt := range map[string]struct {
		img    docker.Image
		facts  imageupdate.Facts
		reason string
	}{
		"up to date":  {alpineOld, imageupdate.Facts{CheckedAt: now, Digest: "sha256:old"}, reasonUpToDate},
		"failed":      {alpineOld, imageupdate.Facts{CheckedAt: now, Failed: true}, reasonUpdateFailed},
		"local build": {docker.Image{ID: "bbbbbbbbbbbb", Repository: "myapp", Tag: "dev"}, imageupdate.Facts{}, reasonLocalBuild},
		"in use":      {used, imageupdate.Facts{CheckedAt: now, Digest: "sha256:new"}, reasonUsedBy(2)},
	} {
		m := onImage(t, tt.img, tt.facts)
		if a := m.imageUpdateAction(); a.Reason != tt.reason {
			t.Errorf("%s: reason = %q, want %q", name, a.Reason, tt.reason)
		}
		if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Get) {
			t.Errorf("%s: G is not greyed", name)
		}
		m = feed(t, m, testutil.Key(keymap.Get))
		if m.footer.Text() != tt.reason {
			t.Errorf("%s: footer = %q, want the reason", name, m.footer.Text())
		}
	}
}

func runUpdate(t *testing.T, m Model) RegistryPullCompleteMsg {
	t.Helper()
	_, cmd := step(t, m, testutil.Key(keymap.Get))
	// G hands the work to the jobs registry; the router would run it.
	for _, msg := range testutil.Msgs(cmd) {
		start, ok := msg.(jobs.StartMsg)
		if !ok {
			continue
		}
		for _, out := range testutil.Msgs(start.Work("")) {
			if done, ok := out.(RegistryPullCompleteMsg); ok {
				return done
			}
		}
	}
	t.Fatal("G started no update")
	return RegistryPullCompleteMsg{}
}

// A new build pulls the same tag and removes the image it replaced, by ID —
// the tag has moved to the new one.
func TestANewBuildIsPulledAndTheOldImageRemoved(t *testing.T) {
	e := &engineStub{newID: "sha256:cccccccccccc0000"}
	stubEngine(t, e)
	m := onImage(t, alpineOld, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:new"})
	done := runUpdate(t, m)
	if want := []string{"ps aaaaaaaaaaaa", "pull alpine:latest", "rmi aaaaaaaaaaaa"}; !slices.Equal(e.calls, want) {
		t.Errorf("calls = %v, want %v", e.calls, want)
	}
	m = feed(t, m, done)
	if !strings.Contains(m.footer.Text(), "old image removed") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

// A newer patch is pulled under its own tag.
func TestANewPatchIsPulledUnderItsTag(t *testing.T) {
	e := &engineStub{newID: "sha256:dddddddddddd"}
	stubEngine(t, e)
	node := docker.Image{ID: "eeeeeeeeeeee", Repository: "node", Tag: "20.11.1", RepoDigests: []string{"node@sha256:n"}}
	m := onImage(t, node, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:n", NewerPatch: "20.11.4"})
	done := runUpdate(t, m)
	if !slices.Contains(e.calls, "pull node:20.11.4") || done.Replaces != "node:20.11.1" {
		t.Errorf("calls = %v, done = %+v", e.calls, done)
	}
	m = feed(t, m, done)
	if !strings.Contains(m.footer.Text(), "node:20.11.1 → node:20.11.4") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

// A container created between the key and the pull stops the update before
// anything is pulled, and the footer names it — the case podman, which reports
// no container count, always goes through.
func TestAnImageInUseIsNotUpdatedAndTheFooterSaysWhy(t *testing.T) {
	e := &engineStub{users: []string{"web", "worker"}}
	stubEngine(t, e)
	m := onImage(t, alpineOld, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:new"})
	done := runUpdate(t, m)
	if slices.ContainsFunc(e.calls, func(c string) bool { return strings.HasPrefix(c, "pull") || strings.HasPrefix(c, "rmi") }) {
		t.Errorf("calls = %v, want nothing pulled or removed", e.calls)
	}
	m = feed(t, m, done)
	if got := m.footer.Text(); !strings.Contains(got, "used by web, worker") {
		t.Errorf("footer = %q, want the containers named", got)
	}
}

// A pull that brings the image already held must not remove it.
func TestAPullThatBringsTheSameImageRemovesNothing(t *testing.T) {
	e := &engineStub{newID: "sha256:aaaaaaaaaaaa1234"}
	stubEngine(t, e)
	m := onImage(t, alpineOld, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:new"})
	done := runUpdate(t, m)
	if slices.ContainsFunc(e.calls, func(c string) bool { return strings.HasPrefix(c, "rmi") }) || !done.Unchanged {
		t.Errorf("calls = %v, done = %+v", e.calls, done)
	}
}

func TestAFailedPullOrRemovalIsReported(t *testing.T) {
	e := &engineStub{pullErr: errors.New("denied")}
	stubEngine(t, e)
	m := onImage(t, alpineOld, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:new"})
	failed := feed(t, m, runUpdate(t, m))
	if got := failed.footer.Text(); !strings.Contains(got, "failed") {
		t.Errorf("pull failure footer = %q", got)
	}

	e.pullErr, e.removeErr, e.newID = nil, errors.New("conflict"), "sha256:ffffffffffff"
	kept := feed(t, m, runUpdate(t, m))
	if got := kept.footer.Text(); !strings.Contains(got, "old image was kept") {
		t.Errorf("removal failure footer = %q", got)
	}
}

// identitiesAsked records which image continuity read identities from.
type identitiesAsked struct{ of *[]string }

func (identitiesAsked) Verify(context.Context, string, trust.Rule) (trust.Verdict, error) {
	return trust.Verified, nil
}

func (v identitiesAsked) Identities(_ context.Context, ref string) ([]trust.Identity, error) {
	*v.of = append(*v.of, ref)
	return nil, nil
}

// A newer patch is compared with the image it replaces (§3.82): the target's
// tag is not on disk yet, so its own name would leave nothing to continue.
func TestAnUpdateContinuesTheImageItReplaces(t *testing.T) {
	e := &engineStub{newID: "sha256:dddddddddddd"}
	stubEngine(t, e)
	var asked []string
	prev := newPullDeps
	newPullDeps = func(*config.Config, *scan.Report) imagepull.Deps {
		return imagepull.Deps{
			Enabled:  true,
			Policy:   func() (trust.Policy, error) { return trust.Policy{}, nil },
			Verifier: identitiesAsked{of: &asked},
			Digest:   func(string) (string, error) { return "sha256:new", nil },
			Current:  func(string) string { return "" },
			Tag:      func(string, string) error { return nil },
		}
	}
	t.Cleanup(func() { newPullDeps = prev })

	node := docker.Image{ID: "eeeeeeeeeeee", Repository: "node", Tag: "20.11.1", RepoDigests: []string{"node@sha256:n"}}
	m := onImage(t, node, imageupdate.Facts{CheckedAt: time.Now(), Digest: "sha256:n", NewerPatch: "20.11.4"})
	runUpdate(t, m)
	if want := []string{"docker.io/library/node@sha256:n"}; !slices.Equal(asked, want) {
		t.Errorf("continuity read %q, want the replaced image %q", asked, want)
	}
}
