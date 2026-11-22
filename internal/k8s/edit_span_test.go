package k8s

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-688 (S-03, step 1) — the lines of one document, for the re-scan that
// judges a workload; and the file's own indentation step, for the lines the fix
// inserts.

func TestDocumentSpanIsTheLinesOfTheDocumentThatHoldsTheLine(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  containers:
    - name: a
      image: a
---
apiVersion: v1
kind: Pod
metadata:
  name: second
spec:
  containers:
    - name: b
      image: b
`
	tests := []struct {
		name string
		line int
		want Span
		ok   bool
	}{
		{"the first line of the first document", 1, Span{1, 8}, true},
		{"the last line of the first document", 8, Span{1, 8}, true},
		{"a line of the second document", 13, Span{10, 17}, true},
		{"the last line of the second document", 17, Span{10, 17}, true},
		{"the separator between the two", 9, Span{}, false},
		{"a line after the file", 40, Span{}, false},
		{"line zero", 0, Span{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DocumentSpan([]byte(src), tt.line)
			if ok != tt.ok || got != tt.want {
				t.Errorf("DocumentSpan(%d) = %+v, %v; want %+v, %v", tt.line, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// A line before the first key (a header comment) is in no document: the fix
// must not judge the whole file by the first one.
func TestDocumentSpanIgnoresALineBeforeTheDocument(t *testing.T) {
	const src = "# header\n# more header\napiVersion: v1\nkind: Pod\nmetadata:\n  name: a\n"
	if span, ok := DocumentSpan([]byte(src), 1); ok {
		t.Errorf("a header comment is in a document: %+v", span)
	}
	if span, ok := DocumentSpan([]byte(src), 3); !ok || span != (Span{3, 6}) {
		t.Errorf("DocumentSpan(3) = %+v, %v; want {3 6}, true", span, ok)
	}
}

func TestIndentStep(t *testing.T) {
	tests := []struct {
		name string
		src  string
		key  string // looked up in the root mapping
		want int
		ok   bool
	}{
		{"two spaces", "spec:\n  a: 1\n  b: 2\n", "spec", 2, true},
		{"four spaces", "spec:\n    a: 1\n    b: 2\n", "spec", 4, true},
		{"a first child that is itself a mapping", "x:\n  spec:\n      a: 1\n", "x", 2, true},
		{"a flow mapping", "spec: {a: 1}\n", "spec", 0, false},
		{"an empty flow mapping", "spec: {}\n", "spec", 0, false},
		{"a scalar", "spec: 3\n", "spec", 0, false},
		{"a null value", "spec:\nother: 1\n", "spec", 0, false},
		{"a sequence", "spec:\n  - a\n", "spec", 0, false},
		{"an indentless sequence", "spec:\n- a\n", "spec", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tt.src), &doc); err != nil {
				t.Fatal(err)
			}
			key, value, found := Entry(doc.Content[0], tt.key)
			if !found {
				t.Fatalf("no key %q", tt.key)
			}
			got, ok := IndentStep(key, value)
			if ok != tt.ok || got != tt.want {
				t.Errorf("IndentStep = %d, %v; want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// The step is measured between the parent key and its first child, whatever
// the column of the parent: a pod spec written at column 7 gives 2 when its
// children are at column 9.
func TestIndentStepIsTheDistanceBetweenTheTwoColumns(t *testing.T) {
	var doc yaml.Node
	src := "a:\n  b:\n    c:\n      spec:\n          x: 1\n"
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	spec := follow(doc.Content[0], []string{"a", "b", "c"})
	key, value, ok := Entry(spec, "spec")
	if !ok {
		t.Fatal("no spec key")
	}
	if got, ok := IndentStep(key, value); !ok || got != 4 {
		t.Errorf("IndentStep = %d, %v; want 4, true (column 7 to column 11)", got, ok)
	}
}
