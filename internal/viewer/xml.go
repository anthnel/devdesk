package viewer

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// parseXML builds the tree from the token stream, for the same reason as the
// JSON parser: order is content.
//
// Two shaping decisions, both about what a reader wants to see rather than what
// the format technically holds:
//
//   - An attribute is a node of its own, a child of its element. Most XML worth
//     reading keeps its information in attributes, and a tree that showed only
//     elements would show almost nothing.
//   - An element whose only content is text carries that text as its own value
//     instead of gaining a child for it. It halves the depth of every document
//     that is mostly leaves, and `<name>nginx</name>` reads as one row because
//     it is one fact.
func parseXML(text string) (*Node, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	ids := &idGen{}

	var root *Node
	var stack []*Node
	// texts holds the character data seen so far for each open element, keyed by
	// its depth in the stack. It is resolved at EndElement, which is the first
	// moment we know whether the element also had element children.
	var texts []string

	for {
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			node := &Node{ID: ids.take(), Key: elementName(t.Name), Kind: NodeElement}
			for _, attr := range t.Attr {
				node.Children = append(node.Children, &Node{
					ID:    ids.take(),
					Key:   "@" + elementName(attr.Name),
					Value: quoteInline(attr.Value),
					Kind:  NodeAttr,
				})
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("a second root element %q", node.Key)
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
			texts = append(texts, "")

		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			if trimmed := strings.TrimSpace(string(t)); trimmed != "" {
				texts[len(texts)-1] += trimmed
			}

		case xml.Comment:
			if len(stack) == 0 {
				continue
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, &Node{
				ID:    ids.take(),
				Key:   "<!--",
				Value: strings.TrimSpace(string(t)),
				Kind:  NodeComment,
			})

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("closing tag %q with nothing open", t.Name.Local)
			}
			node := stack[len(stack)-1]
			closeElement(node, texts[len(texts)-1], ids)
			stack = stack[:len(stack)-1]
			texts = texts[:len(texts)-1]
		}
	}

	if root == nil {
		return nil, fmt.Errorf("no root element")
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("%d unclosed element(s)", len(stack))
	}
	return root, nil
}

// closeElement settles what an element does with the text it collected: it
// becomes the element's own value when there is nothing else to confuse it
// with, and a child otherwise — mixed content is rare, and a row of its own is
// the only honest place for it.
func closeElement(node *Node, text string, ids *idGen) {
	if text == "" {
		return
	}
	if !hasElementChild(node) {
		node.Value = quoteInline(text)
		return
	}
	node.Children = append(node.Children, &Node{
		ID:    ids.take(),
		Key:   "#text",
		Value: quoteInline(text),
		Kind:  NodeText,
	})
}

func hasElementChild(node *Node) bool {
	for _, child := range node.Children {
		if child.Kind == NodeElement {
			return true
		}
	}
	return false
}

// elementName keeps the namespace prefix out of the tree. xml.Decoder resolves
// a prefix to the full namespace URI, so `k8s:name` would render as
// `http://…/v1 name` — a URL in every row, saying the same thing in all of them.
func elementName(name xml.Name) string {
	return name.Local
}
