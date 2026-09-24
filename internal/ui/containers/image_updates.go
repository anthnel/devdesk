package containers

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imageupdate"
)

// The Update column (§3.88): whether the image a container runs has a newer
// version in its registry.
//
// A container is compared through the image it was created from, not through
// the local image its tag names today: after a pull, the container still runs
// the old one until it is recreated, and that is exactly what the arrow is there
// to say.

// containerUpdates is the state behind the column.
type containerUpdates struct {
	tracker imageupdate.Tracker
	// digests are the registry digests of each container's image, by ID.
	digests map[string][]string
	// listed is the set of containers the digests were read for, so they are
	// read again when it changes rather than on every two-second refresh.
	listed string
}

func (u *containerUpdates) status(c docker.Container) imageupdate.Status {
	if strings.HasPrefix(c.Image, "sha256:") {
		return imageupdate.Status{} // created from an image ID: names no registry
	}
	return u.tracker.Status(c.Image, u.digests[c.ID], imageupdate.LocalBuild)
}

// The seams tests replace: no engine and no registry is reached from a test.
var (
	containerImageDigests = docker.ContainerImageDigests
	checkImageUpdates     = func(refs []string) map[string]imageupdate.Facts {
		return imageupdate.Check(imageupdate.Default(), refs, time.Now())
	}
)

// updatesCmd returns the check a new list calls for, or nil: when the set of
// containers changed, or an image's answer is due. It marks what it asks for.
func (u *containerUpdates) updatesCmd(containers []docker.Container, now time.Time) tea.Cmd {
	ids := make([]string, 0, len(containers))
	var refs []string
	for _, c := range containers {
		ids = append(ids, c.ID)
		// A container created from an image ID names no registry.
		if !strings.HasPrefix(c.Image, "sha256:") {
			refs = append(refs, c.Image)
		}
	}
	slices.Sort(ids)
	listed := strings.Join(ids, ",")
	due := u.tracker.Due(refs, now)
	if listed == u.listed && len(due) == 0 {
		return nil
	}
	u.listed = listed
	byImage := map[string][]string{}
	for _, c := range containers {
		byImage[c.Image] = append(byImage[c.Image], c.ID)
	}
	return func() tea.Msg {
		digests := containerImageDigests(ids)
		// Only an image some container runs as a pulled image is worth a
		// registry's time; the others are answered "checked, nothing known" so
		// they are not asked again until stale.
		var pulled []string
		for _, ref := range due {
			for _, id := range byImage[ref] {
				if len(digests[id]) > 0 {
					pulled = append(pulled, ref)
					break
				}
			}
		}
		facts := checkImageUpdates(pulled)
		if facts == nil {
			facts = map[string]imageupdate.Facts{}
		}
		for _, ref := range due {
			if _, ok := facts[ref]; !ok {
				facts[ref] = imageupdate.Facts{CheckedAt: now}
			}
		}
		return ContainerUpdatesCheckedMsg{Digests: digests, Facts: facts}
	}
}

func (m Model) handleContainerUpdatesChecked(msg ContainerUpdatesCheckedMsg) (tea.Model, tea.Cmd) {
	m.updates.digests = msg.Digests
	m.updates.tracker.Store(msg.Facts)
	m.containerTable.SetItems(m.containerTable.Items())
	return m, nil
}
