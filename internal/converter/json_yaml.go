package converter

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// encoderIndent is the indentation width this package encodes with.
// flushSequences depends on knowing it.
const encoderIndent = 2

// ConvertAPIResponseToYAML converts a document read back from Dash0 into the
// YAML that belongs in a resource's `*_yaml` state attribute.
//
// The api-client returns typed definitions, but this provider's client wrappers
// re-serialize them with marshalToJSON, so a resource's Read sees a JSON
// string. Storing that string in an attribute whose configuration side is YAML
// makes every refresh that detects drift render as a full-document replacement
// in `terraform plan`, jsonencode on one side and a YAML heredoc on the other.
//
// referenceYAML is the document the new value replaces, normally the current
// state value. Its key order, quoting, and sequence indentation carry over to
// the result, so the plan shows only the fields that changed. Pass an empty
// string when there is nothing to align against.
//
// JSON is a subset of YAML, so a document that is already YAML is accepted.
func ConvertAPIResponseToYAML(apiResponse, referenceYAML string) (string, error) {
	root, err := parseDocumentNode(apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing API response: %w", err)
	}

	// Drop the flow style and mandatory double quoting the JSON spelling carries
	// into the node tree, so the result reads as block YAML with plain scalars.
	applyBlockStyle(root)

	var reference *yaml.Node
	if referenceYAML != "" {
		// A reference that does not parse is not an error. It only means there is
		// nothing to inherit, so fall through with the API's own order.
		if parsed, refErr := parseDocumentNode(referenceYAML); refErr == nil {
			reference = parsed
			alignKeyOrder(root, reference)
		}
	}

	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(encoderIndent)
	if err := encoder.Encode(root); err != nil {
		return "", fmt.Errorf("error encoding YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("error closing YAML encoder: %w", err)
	}

	result := buf.String()
	if reference != nil && referenceUsesFlushSequences(referenceYAML) {
		if flushed, ok := flushSequences(result); ok && sameDocument(result, flushed) {
			result = flushed
		}
	}

	// Reordering, re-quoting, and re-indenting must not change the document. A
	// value that lost a field would be sent back to the API on the next apply,
	// so verify before returning. Callers treat the error as "keep what the API
	// returned".
	if !sameDocument(apiResponse, result) {
		return "", fmt.Errorf("rendering the document as YAML would have changed it")
	}
	return result, nil
}

// parseDocumentNode parses a single YAML or JSON document and returns its root
// content node.
func parseDocumentNode(document string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(document), &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("document is empty")
	}
	return doc.Content[0], nil
}

// applyBlockStyle clears the styles on a node tree so the encoder emits block
// YAML and picks its own scalar spelling.
func applyBlockStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	node.Style = 0
	for _, child := range node.Content {
		applyBlockStyle(child)
	}
}

// alignKeyOrder reorders the mapping keys of node to follow the order they
// appear in reference, recursively. Keys reference does not carry keep the order
// the API sent them in and sort after every key it does carry.
//
// It also carries the reference's spelling over to any key or scalar whose value
// and resolved tag both match, so a `"views:read"` the user quoted is not
// re-rendered as plain `views:read` and counted as a changed line. Requiring the
// value and tag to match is what makes that safe on arbitrary user YAML: an
// inherited style can only re-spell a scalar that already means the same thing.
//
// Known limitation. Sequences align by position, so an element inserted into or
// removed from the middle of a list shifts every element after it against the
// wrong reference sibling. Those re-render in the API's spelling and show as
// changed even when their content did not move. It lasts one apply, because the
// next Read aligns against what that apply stored. Aligning by identity would
// avoid it, but these documents have no field that identifies an element across
// all six resources.
func alignKeyOrder(node, reference *yaml.Node) {
	if node == nil || reference == nil {
		return
	}

	switch node.Kind {
	case yaml.ScalarNode:
		if reference.Kind == yaml.ScalarNode && reference.Value == node.Value && reference.Tag == node.Tag {
			node.Style = reference.Style
		}
	case yaml.MappingNode:
		if reference.Kind == yaml.MappingNode {
			alignMappingKeyOrder(node, reference)
		}
	case yaml.SequenceNode:
		if reference.Kind != yaml.SequenceNode {
			return
		}
		for i, child := range node.Content {
			if i >= len(reference.Content) {
				break
			}
			alignKeyOrder(child, reference.Content[i])
		}
	}
}

func alignMappingKeyOrder(node, reference *yaml.Node) {
	rank := make(map[string]int, len(reference.Content)/2)
	keys := make(map[string]*yaml.Node, len(reference.Content)/2)
	values := make(map[string]*yaml.Node, len(reference.Content)/2)
	for i := 0; i+1 < len(reference.Content); i += 2 {
		key := reference.Content[i].Value
		if _, duplicate := rank[key]; duplicate {
			continue
		}
		rank[key] = i
		keys[key] = reference.Content[i]
		values[key] = reference.Content[i+1]
	}

	type entry struct {
		key, value *yaml.Node
		rank       int
	}

	entries := make([]entry, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		alignKeyOrder(value, values[key.Value])

		keyRank, known := rank[key.Value]
		if !known {
			keyRank = len(reference.Content) + i
		} else if keys[key.Value] != nil {
			key.Style = keys[key.Value].Style
		}
		entries = append(entries, entry{key: key, value: value, rank: keyRank})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].rank < entries[j].rank })

	node.Content = node.Content[:0]
	for _, e := range entries {
		node.Content = append(node.Content, e.key, e.value)
	}
}

// sameDocument reports whether two YAML texts parse to the same data.
func sameDocument(a, b string) bool {
	var parsedA, parsedB interface{}
	if yaml.Unmarshal([]byte(a), &parsedA) != nil || yaml.Unmarshal([]byte(b), &parsedB) != nil {
		return false
	}
	return reflect.DeepEqual(parsedA, parsedB)
}
