package converter

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const encoderIndent = 2

// ConvertAPIResponseToYAML renders a document read back from Dash0 as the YAML
// to store in a resource's `*_yaml` attribute. The client wrappers hand Read a
// JSON string, and storing that in a YAML attribute makes `terraform plan`
// render every refresh as a full-document replacement. referenceYAML, normally
// the value being replaced, supplies the key order, quoting, and sequence
// indentation that keep the plan to a line-level diff; pass "" for none.
func ConvertAPIResponseToYAML(apiResponse, referenceYAML string) (string, error) {
	root, err := parseDocumentNode(apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing API response: %w", err)
	}

	applyBlockStyle(root)

	var reference *yaml.Node
	if referenceYAML != "" {
		// A reference that does not parse just leaves nothing to inherit.
		if parsed, refErr := parseDocumentNode(referenceYAML); refErr == nil {
			reference = parsed
			if json.Valid([]byte(referenceYAML)) {
				// JSON is nobody's spelling, and every scalar in it is quoted.
				// Inheriting that sticks, since the stored value is the next
				// reference. Take key order only.
				applyBlockStyle(reference)
			}
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

	// Reordering and re-spelling must not change the document, or the next apply
	// sends a different one. Callers treat the error as "keep the API's copy".
	if !sameDocument(apiResponse, result) {
		return "", fmt.Errorf("rendering the document as YAML would have changed it")
	}
	return result, nil
}

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

// applyBlockStyle drops the flow style and forced quoting a JSON source carries,
// leaving the encoder to pick its own spelling.
func applyBlockStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	node.Style = 0
	for _, child := range node.Content {
		applyBlockStyle(child)
	}
}

// alignKeyOrder reorders node's mapping keys to follow reference, recursively.
// Keys reference does not carry sort last, in the order the API sent them.
//
// It also copies reference's spelling onto any key or scalar whose value and tag
// both match, so a `"views:read"` the user quoted does not re-render as plain.
// Matching on both keeps that safe: the style can only re-spell a scalar that
// already means the same thing.
//
// Known limitation: sequences align by position, so inserting or removing a
// middle element shifts the rest against the wrong sibling for one apply. No
// field identifies an element across all six resources, so identity-based
// alignment is not available.
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

// sameDocument reports whether two texts parse to identical data. Unlike
// ResourceYAMLEquivalent it strips nothing: it guards against a lost field.
func sameDocument(a, b string) bool {
	var parsedA, parsedB interface{}
	if yaml.Unmarshal([]byte(a), &parsedA) != nil || yaml.Unmarshal([]byte(b), &parsedB) != nil {
		return false
	}
	return reflect.DeepEqual(parsedA, parsedB)
}
