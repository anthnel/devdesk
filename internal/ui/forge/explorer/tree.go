package explorer

import (
	"strings"
	"time"
)

// NodeType représente le type de nœud dans l'arbre
type NodeType string

const (
	NodeTypeGroup   NodeType = "group"
	NodeTypeProject NodeType = "project"
)

// TreeNode représente un nœud dans l'arbre de la forge.
//
// Il survit à l'abstraction (§3.6) parce que ce qu'il porte en plus est de
// l'état de vue — développé, en cours de chargement, parent, profondeur — dont
// une forge n'a pas d'avis. Ce qu'il ne porte plus est ce que la forge décide :
// l'identifiant est opaque et le rôle est déjà un mot.
type TreeNode struct {
	// ID adresse le nœud auprès du backend et ne veut rien dire ici. C'était un
	// int64 — l'identifiant numérique de GitLab — que quatre sites convertissaient
	// en int pour le repasser au SDK.
	ID       string
	Name     string
	FullPath string
	Type     NodeType
	Children []*TreeNode
	Expanded bool
	Loading  bool // True when loading children
	Parent   *TreeNode
	Depth    int // Depth in tree (for rendering)

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

// IsExpandable retourne vrai si le nœud peut être étendu
func (n *TreeNode) IsExpandable() bool {
	return n.Type == NodeTypeGroup
}

// Toggle expande ou contracte le nœud
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
