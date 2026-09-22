package k8s

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// yaml.v3 counts columns in characters; a byte offset has to walk the runes,
// or the first non-ASCII character before a value shifts every edit after it.
func TestOffsetCountsCharactersNotBytes(t *testing.T) {
	content := []byte("a: 1\nnamé: true\n")
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatal(err)
	}
	_, value, _ := Entry(doc.Content[0], "namé")

	start, end, ok := PlainScalarSpan(content, value)
	if !ok || string(content[start:end]) != "true" {
		t.Errorf("span = %d..%d (%q), want the value", start, end, content[start:end])
	}
}

// A quoted scalar's bytes are not its value, so no span is offered for it.
func TestAQuotedScalarHasNoPlainSpan(t *testing.T) {
	content := []byte("privileged: \"true\"\n")
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatal(err)
	}
	_, value, _ := Entry(doc.Content[0], "privileged")

	if _, _, ok := PlainScalarSpan(content, value); ok {
		t.Error("a quoted scalar was offered as plain")
	}
}

// Only a key that begins its own line has somewhere to insert above it.
func TestOwnLineIndentRefusesTheDashLine(t *testing.T) {
	content := []byte("containers:\n  - name: api\n    image: x\n")
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatal(err)
	}
	c := Containers(doc.Content[0])[0]

	if _, _, ok := OwnLineIndent(content, c.Content[0]); ok {
		t.Error("the key on the dash line was offered as an insertion point")
	}
	if _, indent, ok := OwnLineIndent(content, c.Content[2]); !ok || indent != "    " {
		t.Errorf("image: indent %q ok=%v, want four spaces", indent, ok)
	}
}
