package k8s

import (
	"strings"
	"testing"
)

const threeDocuments = `apiVersion: v1
kind: Pod
metadata:
  name: first
---
# a comment-only document is skipped
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: second
spec:
  template:
    spec:
      containers:
        - name: web
          image: nginx
---
- not
- a mapping
---
apiVersion: v1
kind: Pod
metadata:
  name: third
`

// The index answers every line exactly as DocumentAt does, from one decode.
func TestIndexDocumentsAgreesWithDocumentAt(t *testing.T) {
	content := []byte(threeDocuments)
	index := IndexDocuments(content)
	lines := strings.Count(threeDocuments, "\n") + 3
	for line := -1; line <= lines; line++ {
		want, wantOK := DocumentAt(content, line)
		got, gotOK := index.At(line)
		if gotOK != wantOK {
			t.Fatalf("line %d: found = %v, DocumentAt found = %v", line, gotOK, wantOK)
		}
		if gotOK && got.Line != want.Line {
			t.Errorf("line %d: document at line %d, DocumentAt says %d", line, got.Line, want.Line)
		}
	}
}

func TestIndexDocumentsOfSomethingThatIsNotYAML(t *testing.T) {
	if _, ok := IndexDocuments([]byte("{{ not: [yaml")).At(1); ok {
		t.Error("a document was found in content that is not YAML")
	}
	if _, ok := IndexDocuments(nil).At(1); ok {
		t.Error("a document was found in empty content")
	}
}

// A document that decodes after a broken one is not offered: DocumentAt stops
// at the first decode error, and the index keeps that behavior.
func TestIndexDocumentsStopsAtTheFirstDecodeError(t *testing.T) {
	content := []byte("kind: Pod\nmetadata:\n  name: a\n---\n{{ broken: [\n---\nkind: Pod\nmetadata:\n  name: c\n")
	index := IndexDocuments(content)
	if _, ok := index.At(2); !ok {
		t.Error("the document before the error is lost")
	}
	if _, ok := index.At(8); ok {
		t.Error("a document after the decode error was offered")
	}
}
