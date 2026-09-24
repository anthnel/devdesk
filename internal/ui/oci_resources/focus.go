package ociresources

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
)

// FocusImageRequestMsg asks the router to open this view on the Images tab with
// the cursor on the named image (keymap.Jump from containers). The reference is
// whatever the engine reports as a container's image: a name, a name with an
// implicit tag, a digest reference or a bare ID once the tag has moved on.
type FocusImageRequestMsg struct {
	Image string
}

// handleFocusImage puts the cursor on the requested image.
//
// It waits for the first listing like an agent's request does: the router may
// have just built this view, and an empty table is not yet an answer about
// what the engine holds. The search is dropped when it hides the target — the
// user asked for that row, and a filter they typed earlier elsewhere is not a
// reason to land on a different one.
func (m Model) handleFocusImage(msg FocusImageRequestMsg) (tea.Model, tea.Cmd) {
	if cmd, waiting := m.deferUntilListed(msg); waiting {
		return m, cmd
	}
	updated, cmd := m.switchTab(int(tabImages))
	m = updated.(Model)

	if !m.focusImage(msg.Image) && m.imageTable.FilterBar().SearchQuery() != "" {
		m.imageTable.FilterBar().ClearSearch()
		m.imageTable.SetItems(m.imageTable.Items()) // re-apply with the query gone
		m.focusImage(msg.Image)
	}
	if row, ok := m.imageTable.Selected(); !ok || !imageMatches(row.Image, msg.Image) {
		return m, tea.Batch(cmd, m.footer.Warn("Image "+msg.Image+" is not listed — it may have been removed"))
	}
	return m, cmd
}

// focusImage moves the cursor to the first visible row matching ref and
// reports whether there was one.
func (m *Model) focusImage(ref string) bool {
	for i, row := range m.imageTable.Visible() {
		if imageMatches(row.Image, ref) {
			m.imageTable.SetCursor(i)
			return true
		}
	}
	return false
}

// imageMatches reports whether a container's image reference names img.
//
// The engines do not agree on one spelling: docker keeps `nginx` for a Hub
// image where podman lists `docker.io/library/nginx`, a container created
// without a tag says `nginx` for `nginx:latest`, one created by digest says
// `repo@sha256:…`, and one whose tag has since moved to a newer image shows
// the old image's ID instead of a name.
func imageMatches(img docker.Image, ref string) bool {
	if ref == "" {
		return false
	}
	if repo, digest, ok := strings.Cut(ref, "@"); ok {
		for _, d := range img.RepoDigests {
			r, dd, _ := strings.Cut(d, "@")
			if dd == digest && canonicalRepo(r) == canonicalRepo(repo) {
				return true
			}
		}
		return false
	}
	if isImageID(ref) && img.ID != "" {
		id := strings.TrimPrefix(ref, "sha256:")
		own := strings.TrimPrefix(img.ID, "sha256:")
		return strings.HasPrefix(own, id) || strings.HasPrefix(id, own)
	}
	if img.Repository == "" || img.Repository == "<none>" {
		return false
	}
	repo, tag := splitTag(ref)
	return canonicalRepo(repo) == canonicalRepo(img.Repository) && tag == imageTag(img)
}

// splitTag separates "repo:tag", with "latest" standing in for a missing tag.
// The colon of a registry port ("host:5000/app") is not a tag separator.
func splitTag(ref string) (repo, tag string) {
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i:], "/") {
		return ref, "latest"
	}
	return ref[:i], ref[i+1:]
}

// imageTag is the image's tag, "latest" when the engine reports none.
func imageTag(img docker.Image) string {
	if img.Tag == "" || img.Tag == "<none>" {
		return "latest"
	}
	return img.Tag
}

// canonicalRepo drops the Hub prefixes so docker's `nginx` and podman's
// `docker.io/library/nginx` compare equal.
func canonicalRepo(repo string) string {
	for _, prefix := range []string{"docker.io/library/", "docker.io/", "index.docker.io/library/", "index.docker.io/"} {
		if rest, ok := strings.CutPrefix(repo, prefix); ok {
			return strings.TrimPrefix(rest, "library/")
		}
	}
	return strings.TrimPrefix(repo, "library/")
}

// isImageID reports whether ref is an image ID rather than a name: a bare hex
// string of at least 12 characters, with or without its "sha256:" prefix.
func isImageID(ref string) bool {
	id := strings.TrimPrefix(ref, "sha256:")
	if len(id) < 12 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
