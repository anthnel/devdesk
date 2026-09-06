package explorer

import (
	"strings"
	"time"
)

// NodeType represents the node type in the tree
type NodeType string

const (
	NodeTypeGroup   NodeType = "group"
	NodeTypeProject NodeType = "project"
)

// TreeNode represents a node in the forge's tree.
//
// It survives the abstraction (§3.6) because what it carries beyond that is
// view state — expanded, currently loading, parent, depth — on which a forge
// has no opinion. What it no longer carries is what the forge decides: the
// identifier is opaque and the role is already a word.
type TreeNode struct {
	// ID addresses the node with the backend and means nothing here. It used
	// to be an int64 — GitLab's numeric identifier — that four call sites
	// converted to int to hand it back to the SDK.
	ID       string
	Name     string
	FullPath string
	Type     NodeType
	Children []*TreeNode
	Expanded bool
	Loading  bool // True when loading children
	Parent   *TreeNode
	Depth    int // Depth in tree (for rendering)

	// Creating marks a node the user has asked for and the forge has not
	// confirmed: it is on screen so the request is visible where it was made,
	// but it carries no ID, no WebURL and no role — nothing a real node has.
	//
	// The spinner on its row does *not* come from this flag: it comes from the
	// registry, keyed on FullPath like every other running target. What the
	// flag answers is the narrower question of whether the node is real yet,
	// which is what refuses the actions that need an identifier.
	Creating bool

	// Metadata for flat table display
	Visibility string // "private", "internal", "public"
	// Role is already humanised — "Owner", "Maintainer", … — because the
	// backend humanises it: GitLab's numeric levels and GitHub's words do not
	// align one to one, so a view translating an integer would be translating
	// GitLab's. It replaces AccessLevel and AccessLevelName().
	Role              string
	CreatedAt         *time.Time // Creation date
	LastActivityAt    *time.Time // Last activity date
	PipelineStatus    string     // Last pipeline status ("success", "failed", "running", "pending", etc.)
	MarkedForDeletion bool       // True if the project is already scheduled for deletion
	WebURL            string     // Full HTTPS URL for opening in browser
}

// IsExpandable returns true if the node can be expanded
func (n *TreeNode) IsExpandable() bool {
	return n.Type == NodeTypeGroup
}

// Toggle expands or collapses the node
func (n *TreeNode) Toggle() {
	if n.IsExpandable() {
		n.Expanded = !n.Expanded
	}
}

// nodeSlug returns the last path segment of a GitLab FullPath (the URL slug)
func nodeSlug(fullPath string) string {
	parts := strings.Split(fullPath, "/")
	return parts[len(parts)-1]
}
