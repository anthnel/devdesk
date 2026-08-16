package viewer

// NodeKind is what a tree node holds. JSON and XML share the type because the
// tree that walks them is one component; the kinds themselves do not overlap.
type NodeKind string

const (
	// JSON
	NodeObject NodeKind = "object"
	NodeArray  NodeKind = "array"
	NodeString NodeKind = "string"
	NodeNumber NodeKind = "number"
	NodeBool   NodeKind = "bool"
	NodeNull   NodeKind = "null"

	// XML
	NodeElement NodeKind = "element"
	NodeAttr    NodeKind = "attr"
	NodeText    NodeKind = "text"
	NodeComment NodeKind = "comment"
)

// Node is one entry in the tree.
//
// ID identifies it, and it is an int assigned in document order rather than a
// path like `$.spec.containers[0].name`. The tree is parsed once when the
// document opens and never re-parsed, so the counter is stable for as long as
// anything refers to it — and the view's expansion set is a map[int]bool with
// nothing to keep in step. A path would be a second representation of the same
// identity, with more ways for the two to disagree.
type Node struct {
	ID       int
	Key      string
	Value    string
	Kind     NodeKind
	Children []*Node
}

// HasChildren drives the expand chevron. An empty object is a container with
// nothing to expand into, and offering a chevron for it would promise a level
// that is not there.
func (n *Node) HasChildren() bool {
	return n != nil && len(n.Children) > 0
}

// FlatNode is a node at the depth it renders at.
type FlatNode struct {
	Node  *Node
	Depth int
}

// Flatten returns the rows visible for a given set of collapsed nodes, in
// document order.
//
// It is here rather than in the view because it is pure: a tree and a set in,
// a slice out. The set itself is the view's, because being collapsed is a fact
// about the screen and not about the document.
func Flatten(root *Node, collapsed map[int]bool) []FlatNode {
	if root == nil {
		return nil
	}
	var out []FlatNode
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		out = append(out, FlatNode{Node: n, Depth: depth})
		if collapsed[n.ID] {
			return
		}
		for _, child := range n.Children {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// idGen hands out node identities during a parse. It is a type rather than a
// bare *int so the two parsers cannot disagree about whether it pre- or
// post-increments.
type idGen struct{ next int }

func (g *idGen) take() int {
	id := g.next
	g.next++
	return id
}
