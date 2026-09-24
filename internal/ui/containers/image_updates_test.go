package containers

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imageupdate"
)

// stubUpdates answers the two seams for one test and counts registry checks.
func stubUpdates(t *testing.T, digests map[string][]string, facts map[string]imageupdate.Facts) *[][]string {
	t.Helper()
	var asked [][]string
	prevDigests, prevCheck := containerImageDigests, checkImageUpdates
	containerImageDigests = func([]string) map[string][]string { return digests }
	checkImageUpdates = func(refs []string) map[string]imageupdate.Facts {
		asked = append(asked, refs)
		out := map[string]imageupdate.Facts{}
		for _, r := range refs {
			out[r] = facts[r]
		}
		return out
	}
	t.Cleanup(func() { containerImageDigests, checkImageUpdates = prevDigests, prevCheck })
	return &asked
}

func TestAContainerOnAnOutdatedImageShowsTheArrow(t *testing.T) {
	now := time.Now()
	asked := stubUpdates(t,
		map[string][]string{"c1": {"nginx@sha256:old"}}, // c2 runs a built image
		map[string]imageupdate.Facts{"nginx:latest": {CheckedAt: now, Digest: "sha256:new"}},
	)
	list := ContainersListMsg{Containers: []docker.Container{
		{ID: "c1", Name: "web", Image: "nginx:latest", State: "running"},
		{ID: "c2", Name: "app", Image: "myapp:dev", State: "running"},
	}}
	m := newTestModel(t)
	next, cmd := m.Update(list)
	if cmd == nil {
		t.Fatal("a first list asked nothing")
	}
	m = feed(t, next.(Model), cmd())

	if want := [][]string{{"nginx:latest"}}; !reflect.DeepEqual(*asked, want) {
		t.Errorf("registry asked %v, want only the pulled image", *asked)
	}
	web := m.updates.status(docker.Container{ID: "c1", Image: "nginx:latest"})
	if web.Kind != imageupdate.NewBuild || !strings.Contains(web.Label(), "new build") {
		t.Errorf("web = %+v, want a new build", web)
	}
	if app := m.updates.status(docker.Container{ID: "c2", Image: "myapp:dev"}); app.Available() {
		t.Errorf("a built image shows an update: %+v", app)
	}

	// The two-second refresh brings the same containers: nothing is asked.
	if _, cmd := m.Update(list); cmd != nil {
		t.Error("an unchanged list asked again")
	}
}
