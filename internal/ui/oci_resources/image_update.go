package ociresources

import (
	"context"
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Updating an image — G on the Images tab (§3.88).
//
// G pulls what the Update column points at — the newer patch tag, or the same
// tag's new build — then removes the image it replaces. It refuses while any
// container, running or not, was created from that image: removing it would
// break the container, and keeping it would leave the update half done. The
// check is made twice: from the engine's container count before the key (to
// grey G), and against the engine itself just before the pull, since a
// container can be created in between and podman reports no count.

// The reasons G does not apply (Rule 130).
const (
	reasonUpToDate       = "This image is already up to date"
	reasonUpdateFailed   = "The registry did not answer — no update is known (check logs)"
	reasonLocalBuild     = "Built or loaded here — there is no registry to update it from"
	reasonUntagged       = "An untagged image has nothing to update"
	reasonNoUpdate       = "No update is known for this image"
	reasonUpdateChecking = "Still checking the registry — try again in a moment"
)

// reasonUsedBy is the refusal when containers use the image.
func reasonUsedBy(n int) string {
	return fmt.Sprintf("Used by %d container(s) — remove them before updating", n)
}

// imageUpdateAction reports whether G applies to the selected image.
//
// Pending is not a no: the answer is seconds away, and greying G only to light
// it again reads as a glitch. The handler says so if it is pressed meanwhile.
func (m Model) imageUpdateAction() shortcut.Availability {
	if act := m.imageActions(); !act.Enabled() {
		return act
	}
	img := m.getSelectedImage()
	if img.ID == "" {
		return shortcut.Unavailable(reasonNoImage) // a pull's placeholder row
	}
	switch m.imageUpdate(*img).Kind {
	case imageupdate.NewPatch, imageupdate.NewBuild, imageupdate.Pending:
	case imageupdate.UpToDate:
		return shortcut.Unavailable(reasonUpToDate)
	case imageupdate.Failed:
		return shortcut.Unavailable(reasonUpdateFailed)
	case imageupdate.LocalBuild:
		return shortcut.Unavailable(reasonLocalBuild)
	case imageupdate.None:
		return shortcut.Unavailable(reasonUntagged)
	default:
		return shortcut.Unavailable(reasonNoUpdate)
	}
	if img.Containers > 0 {
		return shortcut.Unavailable(reasonUsedBy(img.Containers))
	}
	return shortcut.Availability{}
}

// updateSelectedImage starts the update of the selected image.
func (m Model) updateSelectedImage() (tea.Model, tea.Cmd) {
	if a := m.imageUpdateAction(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	img := *m.getSelectedImage()
	status := m.imageUpdate(img)
	if status.Kind == imageupdate.Pending {
		return m, m.footer.Warn(reasonUpdateChecking)
	}
	target := img.Name()
	if status.Kind == imageupdate.NewPatch {
		target = img.Repository + ":" + status.Tag
	}
	if m.pullingImage(target) {
		return m, m.footer.Warn("Pull already in progress")
	}
	return m, jobs.Start(m.pullRun(target), updateImageCmd(target, img))
}

// The engine calls an update makes, as variables so tests run without one.
var (
	containersUsingImage = docker.ContainersUsingImage
	pullImageContext     = docker.PullImageContext
	imageIDOf            = docker.ImageID
	removeImage          = docker.RemoveImage
)

// updateImageCmd pulls target and removes old, reporting through the pull's
// own pair of messages so the row spins and `K` can stop the pull.
func updateImageCmd(target string, old docker.Image) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	return tea.Sequence(
		func() tea.Msg { return RegistryPullStartingMsg{ImageName: target, Cancel: cancel} },
		func() tea.Msg {
			defer cancel()
			done := RegistryPullCompleteMsg{ImageName: target, Replaces: old.Name()}
			users, err := containersUsingImage(old.ID)
			if err != nil {
				done.Err = err
				return done
			}
			if len(users) > 0 {
				done.InUse = users
				return done
			}
			if done.Err = pullImageContext(ctx, target); done.Err != nil {
				return done
			}
			// The same tag pulled again can bring the same image: removing
			// "the old one" would then remove the one just pulled.
			if id, err := imageIDOf(target); err == nil && docker.SameImageID(id, old.ID) {
				done.Unchanged = true
				return done
			}
			done.RemoveErr = removeImage(old.ID, false)
			return done
		},
	)
}

// handleImageUpdated reports how an update ended. Every refusal and failure
// says why in the footer.
func (m Model) handleImageUpdated(msg RegistryPullCompleteMsg) (tea.Model, tea.Cmd) {
	switch {
	case len(msg.InUse) > 0:
		return m, m.footer.Warn(fmt.Sprintf("Cannot update %s: used by %s — remove them first",
			msg.Replaces, containerList(msg.InUse)))
	case msg.Err != nil:
		log.Printf("ERROR [oci_resources] update %s to %s: %v", msg.Replaces, msg.ImageName, msg.Err)
		return m, m.footer.Error("Update of " + msg.Replaces + " failed — check logs")
	case msg.Unchanged:
		return m, tea.Batch(fetchImages(), m.footer.Info(msg.ImageName+" is already the newest — nothing replaced"))
	case msg.RemoveErr != nil:
		log.Printf("ERROR [oci_resources] remove %s after update: %v", msg.Replaces, msg.RemoveErr)
		return m, tea.Batch(fetchImages(), m.footer.Warn("Pulled "+msg.ImageName+", but the old image was kept — check logs"))
	}
	text := "Updated " + msg.ImageName + " — old image removed"
	if msg.Replaces != msg.ImageName {
		text = "Updated " + msg.Replaces + " → " + msg.ImageName + " — old image removed"
	}
	return m, tea.Batch(fetchImages(), m.footer.Info(text))
}

// containerList names up to three containers, and counts the rest.
func containerList(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
}
