package viewer

import (
	"strings"
	"testing"
)

const nestedJSON = `{"a":{"b":1,"c":2},"d":3}`

func TestFlattenWalksInDocumentOrder(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(nestedJSON))

	var keys []string
	for _, flat := range Flatten(doc.Root, nil) {
		keys = append(keys, flat.Node.Key)
	}
	// The root carries no key; then a, its two children, then d.
	if got := strings.Join(keys, ","); got != ",a,b,c,d" {
		t.Errorf("flattened keys = %q, want \",a,b,c,d\"", got)
	}
}

func TestFlattenReportsTheDepthARowRendersAt(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(nestedJSON))

	depths := map[string]int{}
	for _, flat := range Flatten(doc.Root, nil) {
		depths[flat.Node.Key] = flat.Depth
	}
	for key, want := range map[string]int{"a": 1, "b": 2, "c": 2, "d": 1} {
		if depths[key] != want {
			t.Errorf("depth of %q = %d, want %d", key, depths[key], want)
		}
	}
}

func TestCollapsingANodeHidesItsDescendants(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(nestedJSON))

	a := doc.Root.Children[0]
	collapsed := map[int]bool{a.ID: true}

	var keys []string
	for _, flat := range Flatten(doc.Root, collapsed) {
		keys = append(keys, flat.Node.Key)
	}
	if got := strings.Join(keys, ","); got != ",a,d" {
		t.Errorf("flattened keys = %q, want \",a,d\" — b and c are inside a", got)
	}
}

func TestCollapsingTheRootLeavesOneRow(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(nestedJSON))

	rows := Flatten(doc.Root, map[int]bool{doc.Root.ID: true})
	if len(rows) != 1 {
		t.Errorf("got %d rows with the root collapsed, want 1", len(rows))
	}
}

// Identity is an int assigned in document order, and every node gets a distinct
// one. The view's expansion set is keyed on it, so a collision would collapse
// two unrelated branches together.
func TestEveryNodeHasADistinctID(t *testing.T) {
	doc := Open("n.json", KindAuto, []byte(`{"a":{"b":[1,2,{"c":3}]},"d":null}`))

	seen := map[int]bool{}
	for _, flat := range Flatten(doc.Root, nil) {
		if seen[flat.Node.ID] {
			t.Fatalf("node ID %d appears twice", flat.Node.ID)
		}
		seen[flat.Node.ID] = true
	}
	if len(seen) != doc.NodeCount() {
		t.Errorf("flatten yielded %d nodes, NodeCount says %d", len(seen), doc.NodeCount())
	}
}

// An empty container has nothing to expand into, and a chevron promising a
// level that is not there is worse than no chevron.
func TestAnEmptyContainerOffersNoChevron(t *testing.T) {
	doc := Open("e.json", KindAuto, []byte(`{"empty":{},"list":[],"full":{"a":1}}`))

	for _, tc := range []struct {
		key  string
		want bool
	}{{"empty", false}, {"list", false}, {"full", true}} {
		node := childNamed(t, doc.Root, tc.key)
		if got := node.HasChildren(); got != tc.want {
			t.Errorf("%q HasChildren = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestFlattenOfNothingIsNothing(t *testing.T) {
	if rows := Flatten(nil, nil); rows != nil {
		t.Errorf("Flatten(nil) = %v, want nil", rows)
	}
}

// A value carrying a newline would end its row halfway and push the rest onto a
// line of its own.
func TestAStringValueIsEscapedToOneLine(t *testing.T) {
	doc := Open("s.json", KindAuto, []byte(`{"k":"first\nsecond\ttabbed"}`))

	value := childNamed(t, doc.Root, "k").Value
	if strings.ContainsAny(value, "\n\t") {
		t.Errorf("value %q still holds a raw control character", value)
	}
	if value != `"first\nsecond\ttabbed"` {
		t.Errorf("value = %q, want the escapes made visible", value)
	}
}
