package viewer

import "testing"

const podXML = `<?xml version="1.0"?>
<pod name="web" replicas="3">
  <!-- the front end -->
  <image>nginx:latest</image>
  <ports>
    <port protocol="tcp">8080</port>
  </ports>
</pod>`

func TestXMLAttributesAreNodesOfTheirOwn(t *testing.T) {
	doc := Open("pod.xml", KindAuto, []byte(podXML))
	if doc.Kind != KindXML {
		t.Fatalf("Kind = %q, want xml (ParseErr=%v)", doc.Kind, doc.ParseErr)
	}

	root := doc.Root
	if root.Key != "pod" {
		t.Fatalf("root = %q, want pod", root.Key)
	}

	attrs := map[string]string{}
	for _, child := range root.Children {
		if child.Kind == NodeAttr {
			attrs[child.Key] = child.Value
		}
	}
	if attrs["@name"] != `"web"` {
		t.Errorf("@name = %q, want \"web\" — most XML keeps its information in attributes", attrs["@name"])
	}
	if attrs["@replicas"] != `"3"` {
		t.Errorf("@replicas = %q, want \"3\"", attrs["@replicas"])
	}
}

// An element whose only content is text carries it, rather than gaining a child
// for it: <image>nginx</image> is one fact and should read as one row.
func TestAnElementWithOnlyTextCarriesItAsItsValue(t *testing.T) {
	doc := Open("pod.xml", KindAuto, []byte(podXML))

	image := childNamed(t, doc.Root, "image")
	if image.Value != `"nginx:latest"` {
		t.Errorf("image value = %q, want the text inline", image.Value)
	}
	for _, child := range image.Children {
		if child.Kind == NodeText {
			t.Error("image gained a #text child; the text belongs on the element")
		}
	}
}

func TestAnXMLCommentIsKept(t *testing.T) {
	doc := Open("pod.xml", KindAuto, []byte(podXML))
	for _, child := range doc.Root.Children {
		if child.Kind == NodeComment && child.Value == "the front end" {
			return
		}
	}
	t.Error("the comment is missing from the tree")
}

func TestNestedElementsKeepTheirDepth(t *testing.T) {
	doc := Open("pod.xml", KindAuto, []byte(podXML))

	ports := childNamed(t, doc.Root, "ports")
	port := childNamed(t, ports, "port")
	if port.Value != `"8080"` {
		t.Errorf("port value = %q, want \"8080\"", port.Value)
	}
	if childNamed(t, port, "@protocol").Value != `"tcp"` {
		t.Error("the nested element lost its attribute")
	}
}

func TestAMalformedXMLOpensAsText(t *testing.T) {
	doc := Open("bad.xml", KindAuto, []byte(`<a><b></a>`))
	if doc.Kind != KindPlain {
		t.Errorf("Kind = %q, want plain", doc.Kind)
	}
	if doc.ParseErr == nil {
		t.Error("ParseErr is nil; the extension declared XML")
	}
}

func childNamed(t *testing.T, parent *Node, key string) *Node {
	t.Helper()
	for _, child := range parent.Children {
		if child.Key == key {
			return child
		}
	}
	t.Fatalf("%q has no child %q", parent.Key, key)
	return nil
}
