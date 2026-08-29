package ociresources

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Ce que la ligne sélectionnée permet, calculé une fois et lu par les deux
// moitiés de la vue : GetShortcuts pour griser, les handlers pour refuser
// (Rule 130). Deux calculs pour une question finissent par ne plus dire la
// même chose, et le symptôme est une touche grisée qui agit quand même.

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
