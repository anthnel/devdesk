package explorer

import "time"

// NodeType représente le type de nœud dans l'arbre
type NodeType string

const (
	NodeTypeGroup   NodeType = "group"
	NodeTypeProject NodeType = "project"
)

// TreeNode représente un nœud dans l'arbre GitLab
type TreeNode struct {
	ID       int64 // GitLab uses int64 for IDs
	Name     string
	FullPath string
	Type     NodeType
	Children []*TreeNode
	Expanded bool
	Loading  bool // True when loading children
	Parent   *TreeNode
	Depth    int // Depth in tree (for rendering)

	// Metadata for flat table display
	Visibility        string     // "private", "internal", "public"
	AccessLevel       int        // User's access level (0=none, 10=guest, 20=reporter, 30=dev, 40=maintainer, 50=owner)
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

// AccessLevelName returns a human-readable role name for the access level
func (n *TreeNode) AccessLevelName() string {
	switch {
	case n.AccessLevel >= 50:
		return "Owner"
	case n.AccessLevel >= 40:
		return "Maintainer"
	case n.AccessLevel >= 30:
		return "Developer"
	case n.AccessLevel >= 20:
		return "Reporter"
	case n.AccessLevel >= 10:
		return "Guest"
	default:
		return ""
	}
}
