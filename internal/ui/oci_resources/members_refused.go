package ociresources

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-510 (NX-07): everything a screen says about a group whose members the
// manager refused to list. The wording lives here and nowhere else, as pure
// functions of one value, so a change of text is a change of this file.
//
// None of it carries the manager's error text, a status line from the
// management API or the registry URL: the state is a word and an age, and the
// way out is repo_prefix.

// emptyGroupText is the status line of a group that answered with no member.
const emptyGroupText = "No member found — this group is empty"

// groupRefusalView is everything the refusal wording may use.
type groupRefusalView struct {
	Refusal  cache.RegistryGroupRefusal // Status, Anonymous, At
	Known    int                        // members last listed and still cached (0 if never listed)
	Declared int                        // config entries whose parent is this group (CA5)
}

// refusedReason is the head of every refusal sentence: it says the members could
// not be read, and for whom.
func (v groupRefusalView) refusedReason() string {
	if v.Refusal.Anonymous {
		return "Members could not be read without credentials"
	}
	return "Members could not be read with this account"
}

// declareAdvice is what the user can do about it.
func (v groupRefusalView) declareAdvice() string {
	if v.Refusal.Anonymous {
		return "set credentials or declare them by hand with repo_prefix"
	}
	return "declare them by hand with repo_prefix"
}

// refusedFooterText is the footer line after a refused refresh (H1, level Warn).
func refusedFooterText(v groupRefusalView) string { return refusedStatusLine(v) }

// refusedMembersCell is the Members column of a refused group (H2), plain text
// (Rule 122). The state comes first; the count last known stays visible, since a
// refusal does not empty the cache (D23); the age is the refusal's.
//
// Formats: "denied · <age>" when nothing was ever listed, "denied · N known ·
// <age>" otherwise, <age> being theme.TimeAgo (Rule 127), at most 11 cells, so at
// most 31 for 99 members. State first: the column holds 16 cells at its
// narrowest and cuts the end, so "denied" is what stays.
func refusedMembersCell(v groupRefusalView) string {
	if v.Known > 0 {
		return fmt.Sprintf("denied · %d known · %s", v.Known, timeAgo(v.Refusal.At))
	}
	return "denied · " + timeAgo(v.Refusal.At)
}

// refusedStatusLine is the status line while a refused group row is selected (H3).
func refusedStatusLine(v groupRefusalView) string {
	if v.Declared > 0 {
		return fmt.Sprintf("%s — %d declared by hand are used", v.refusedReason(), v.Declared)
	}
	return v.refusedReason() + " — " + v.declareAdvice()
}

// refusedDrillInText is the footer warning when → has no member to open (H4).
// ctrl+r cannot help after a refusal, so it is not offered.
func refusedDrillInText(v groupRefusalView) string {
	return "No members to open — " + v.declareAdvice()
}

// groupRefusal returns the refusal that still holds for a group, with the counts
// the wording uses. A refusal no more recent than the listing is stale: the
// listing was read after it, so it is ignored.
func (m *Model) groupRefusal(slug string) (groupRefusalView, bool) {
	refusal, ok := m.groupRefusals[slug]
	if !ok {
		return groupRefusalView{}, false
	}
	entry, listed := m.groupCache[slug]
	if listed && !refusal.At.After(entry.DiscoveredAt) {
		return groupRefusalView{}, false
	}
	return m.refusalView(slug, refusal), true
}

// refusalView gathers what the wording may use about one group's refusal.
func (m *Model) refusalView(slug string, r cache.RegistryGroupRefusal) groupRefusalView {
	return groupRefusalView{
		Refusal:  r,
		Known:    len(m.groupCache[slug].Members),
		Declared: declaredMembers(m.registries, slug),
	}
}

// groupMembersStatus is the state line of the selected group row on the
// Registries tab: a refusal (Warn) or a settled empty group (Info). It is
// derived on every render, so it holds while the row is selected and goes when
// another is.
func (m Model) groupMembersStatus() (string, sharedcomponents.Level) {
	if m.activeTab != tabRegistries || m.registryGroupSlug != "" {
		return "", sharedcomponents.LevelInfo
	}
	reg := m.getSelectedRegistry()
	if reg == nil || reg.Kind != config.KindGroup {
		return "", sharedcomponents.LevelInfo
	}
	if v, ok := m.groupRefusal(reg.Slug); ok {
		return refusedStatusLine(v), sharedcomponents.LevelWarning
	}
	// Members declared by hand are members: the group is not empty for them.
	if entry, ok := m.groupCache[reg.Slug]; ok && len(entry.Members) == 0 && declaredMembers(m.registries, reg.Slug) == 0 {
		return emptyGroupText, sharedcomponents.LevelInfo
	}
	return "", sharedcomponents.LevelInfo
}
