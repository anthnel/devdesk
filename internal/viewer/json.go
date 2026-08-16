package viewer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// parseJSON builds the tree from a token stream rather than from a decoded
// value.
//
// The reason is the one thing a JSON viewer must not get wrong: order.
// Unmarshalling into map[string]any loses the order the file was written in,
// and a configuration read back with its keys alphabetised is a different
// document to the one on disk. json.Decoder.Token() hands them over in the
// order they appear, which is also why the tree cannot be sorted afterwards
// (the columns declare no comparator).
func parseJSON(text string) (*Node, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber() // a float64 round-trip would rewrite 1e9 and lose precision

	ids := &idGen{}
	root, err := parseJSONValue(dec, "", ids)
	if err != nil {
		return nil, err
	}

	// A second value after the first is not a JSON document. Without this, a
	// file holding `{"a":1} garbage` would open as a tree showing only the part
	// that happened to parse.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing content after the top-level value")
	}
	return root, nil
}

func parseJSONValue(dec *json.Decoder, key string, ids *idGen) (*Node, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return parseJSONToken(dec, token, key, ids)
}

func parseJSONToken(dec *json.Decoder, token json.Token, key string, ids *idGen) (*Node, error) {
	node := &Node{ID: ids.take(), Key: key}

	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			node.Kind = NodeObject
			if err := parseJSONObject(dec, node, ids); err != nil {
				return nil, err
			}
			node.Value = fmt.Sprintf("{%d}", len(node.Children))
		case '[':
			node.Kind = NodeArray
			if err := parseJSONArray(dec, node, ids); err != nil {
				return nil, err
			}
			node.Value = fmt.Sprintf("[%d]", len(node.Children))
		default:
			return nil, fmt.Errorf("unexpected %q", t)
		}

	case string:
		node.Kind = NodeString
		node.Value = quoteInline(t)

	case json.Number:
		node.Kind = NodeNumber
		node.Value = t.String()

	case bool:
		node.Kind = NodeBool
		node.Value = fmt.Sprintf("%t", t)

	case nil:
		node.Kind = NodeNull
		node.Value = "null"

	default:
		return nil, fmt.Errorf("unsupported token %T", token)
	}

	return node, nil
}

func parseJSONObject(dec *json.Decoder, parent *Node, ids *idGen) error {
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("object key is %T, not a string", keyToken)
		}
		child, err := parseJSONValue(dec, key, ids)
		if err != nil {
			return err
		}
		parent.Children = append(parent.Children, child)
	}
	_, err := dec.Token() // the closing brace
	return err
}

func parseJSONArray(dec *json.Decoder, parent *Node, ids *idGen) error {
	for index := 0; dec.More(); index++ {
		child, err := parseJSONValue(dec, fmt.Sprintf("[%d]", index), ids)
		if err != nil {
			return err
		}
		parent.Children = append(parent.Children, child)
	}
	_, err := dec.Token() // the closing bracket
	return err
}

// quoteInline renders a string value as one row's worth of text.
//
// The quotes stay: without them a string "42" and the number 42 read
// identically, and colour alone should not have to carry that. The escapes
// matter more — an unescaped newline inside a value would end the row halfway
// and push every column after it onto a line of its own.
func quoteInline(s string) string {
	replacer := strings.NewReplacer(
		"\\", `\\`,
		"\"", `\"`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(s) + `"`
}
