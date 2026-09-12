package ociresources

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// What the selected row allows, computed once and read by both halves of
// the view: GetShortcuts to gray out, the handlers to refuse (Rule 130).
// Two computations for one question eventually stop agreeing, and the
// symptom is a grayed-out key that acts anyway.

// The reasons, written once so the header, the footer and the tests cannot
// drift apart on the wording (Rule 129 — English US).
const (
	reasonNoImage      = "No image selected"
	reasonImageScanned = "This image is being scanned"
	reasonNoRegistry   = "No registry selected"
	reasonInsideGroup  = "These rows are discovered members — edit the group instead"
	reasonNotAGroup    = "This entry is a registry, not a group"
	reasonAtTopLevel   = "Already at the registry list"
	reasonNoTags       = "No scan results for this tag yet"
	reasonNoScanner    = "Trivy is not available — check scan settings"
)

// imageActions reports whether N, S and D apply to the selected image.
//
// One state for the three because they share one condition: a row that is not
// in the middle of a scan. A scan reads the image while a delete removes it,
// and launching from an image being scanned is the same race one step removed.
func (m Model) imageActions() shortcut.Availability {
	img := m.getSelectedImage()
	switch {
	case img == nil:
		return shortcut.Unavailable(reasonNoImage)
	case m.scanningImage(img.Name()):
		return shortcut.Unavailable(reasonImageScanned)
	}
	return shortcut.Availability{}
}

// imageScan reports whether S applies to the selected image.
//
// It is imageActions plus one condition that belongs to S alone: scanning an
// *image* from a container means inspecting one the host engine holds, which
// needs that engine's socket mounted into Trivy. Rootless podman has none
// unless `podman system service` is running (§3.67), and that is knowable
// before the keypress — so it is greyed with its reason rather than attempted
// and failed (Rule 130).
//
// N and D do not consult it: launching and deleting go through the engine's
// CLI, not through a container, and greying them for a missing socket would
// refuse actions that work.
//
// Not knowing is not knowing it is a no: while deps is nil nothing has looked
// yet, and greying S out for three frames to un-grey it afterwards reads as a
// fault. Same reasoning as workspaces' scannerState.
func (m Model) imageScan() shortcut.Availability {
	if act := m.imageActions(); !act.Enabled() {
		return act
	}
	if m.deps == nil {
		return shortcut.Availability{}
	}
	if !m.deps.TrivyAvailable {
		return shortcut.Unavailable(reasonNoScanner)
	}
	if m.deps.TrivySource != scan.ToolSourceContainer {
		// A Trivy binary reads the image through the engine itself and needs
		// no socket of its own.
		return shortcut.Availability{}
	}
	// The server address goes through OptionsFromConfig rather than being read
	// off the config: it is ignored unless the client-server checkbox is on,
	// and a second reading of that rule is a second chance to get it wrong.
	server := scan.OptionsFromConfig(m.config).TrivyServer
	if reason := m.deps.ImageScanBlocked(engine.Current().Name, server); reason != "" {
		return shortcut.Unavailable(reason)
	}
	return shortcut.Availability{}
}

// imageOpen reports whether enter has an image to open the details of.
func (m Model) imageOpen() shortcut.Availability {
	if m.getSelectedImage() == nil {
		return shortcut.Unavailable(reasonNoImage)
	}
	return shortcut.Availability{}
}

// registryEntryActions reports whether the four config actions apply.
//
// Inside a group the rows are cached members rather than config entries: there
// is nothing to edit, log into or remove, and nothing to add beside them.
func (m Model) registryEntryActions() shortcut.Availability {
	if m.registryGroupSlug != "" {
		return shortcut.Unavailable(reasonInsideGroup)
	}
	if m.getSelectedRegistry() == nil {
		return shortcut.Unavailable(reasonNoRegistry)
	}
	return shortcut.Availability{}
}

// registryCreate reports whether N applies. It differs from the three others in
// that it needs no selected row — only a list that accepts new entries.
func (m Model) registryCreate() shortcut.Availability {
	if m.registryGroupSlug != "" {
		return shortcut.Unavailable(reasonInsideGroup)
	}
	return shortcut.Availability{}
}

// registryDrillIn reports whether → has a group to enter.
func (m Model) registryDrillIn() shortcut.Availability {
	if m.registryGroupSlug != "" {
		return shortcut.Unavailable(reasonInsideGroup)
	}
	reg := m.getSelectedRegistry()
	if reg == nil || reg.Kind != config.KindGroup {
		return shortcut.Unavailable(reasonNotAGroup)
	}
	return shortcut.Availability{}
}

// registryDrillOut reports whether ← has a level to leave.
func (m Model) registryDrillOut() shortcut.Availability {
	if m.registryGroupSlug == "" {
		return shortcut.Unavailable(reasonAtTopLevel)
	}
	return shortcut.Availability{}
}
