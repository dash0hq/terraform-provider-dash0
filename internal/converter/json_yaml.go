package converter

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConvertAPIResponseToYAML converts a document read back from Dash0 into the
// YAML that belongs in a resource's `*_yaml` state attribute.
//
// The api-client returns typed definitions, but this provider's client wrappers
// re-serialize them with marshalToJSON, so a resource's Read sees a JSON
// string. Storing that string verbatim in an attribute whose configuration side
// is YAML makes every refresh that detects drift render as a full-document
// replacement in `terraform plan`, `jsonencode({ ... })` on one side and a YAML
// heredoc on the other, so a single changed label is indistinguishable from a
// rewrite of the whole resource.
//
// referenceYAML is the document the new value replaces, normally the current
// state value. Its key order is carried over to the result, so the rendered
// diff is limited to the fields that actually changed. Pass an empty string
// when there is nothing to align against (import, or an attribute that is not
// populated yet); the API's own key order is used then.
//
// JSON is a subset of YAML, so a document that is already YAML is accepted and
// reformatted rather than rejected.
func ConvertAPIResponseToYAML(apiResponse, referenceYAML string) (string, error) {
	root, err := parseDocumentNode(apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing API response: %w", err)
	}

	// Drop the flow style and the mandatory double quoting that the JSON source
	// spelling carries into the node tree, so the result reads as block YAML
	// with plain scalars wherever that round-trips unchanged.
	applyBlockStyle(root)

	if referenceYAML != "" {
		// A reference that does not parse is not an error: it only means there
		// is no key order to inherit, so fall through with the API's order.
		if reference, refErr := parseDocumentNode(referenceYAML); refErr == nil {
			alignKeyOrder(root, reference)
		}
	}

	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return "", fmt.Errorf("error encoding YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("error closing YAML encoder: %w", err)
	}

	return buf.String(), nil
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

// applyBlockStyle clears the scalar and collection styles on a node tree so the
// encoder emits block YAML. Multi-line strings are marked as literal blocks;
// the encoder falls back to double quoting on its own for the values a literal
// block cannot represent, such as a line with trailing whitespace.
func applyBlockStyle(node *yaml.Node) {
	if node == nil {
		return
	}

	node.Style = 0
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && strings.Contains(node.Value, "\n") {
		node.Style = yaml.LiteralStyle
	}

	for _, child := range node.Content {
		applyBlockStyle(child)
	}
}

// alignKeyOrder reorders the mapping keys of node to follow the order they
// appear in reference, recursively. Keys that reference does not carry keep the
// order the API sent them in and sort after every key it does carry.
//
// It also carries the reference's spelling over to any scalar whose value and
// resolved tag both match, so a value the user wrote as `"views:read"` is not
// rendered as plain `views:read` and counted as a changed line by the plan.
// Requiring the value and the tag to match is what makes it safe to take the
// spelling from reference, which is arbitrary user-authored YAML: an inherited
// style can only ever re-spell a scalar that already round-trips to the same
// value.
//
// Known limitation. Sequences align by position, so an element inserted into or
// removed from the middle of a list shifts every element after it against the
// wrong reference sibling. Those elements then re-render in the API's key order
// and spelling, and the plan shows them as changed even when their content did
// not move. The noise lasts one apply, because the next Read aligns against the
// value that apply stored. Aligning list elements by a stable identity would
// avoid it, but the documents this handles have no field that reliably
// identifies an element across all six resources.
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
		if reference.Kind != yaml.MappingNode {
			return
		}
		alignMappingKeyOrder(node, reference)
	case yaml.SequenceNode:
		if reference.Kind != yaml.SequenceNode {
			return
		}
		// Sequences align by position. The provider does not reorder lists, and
		// the API returns them in the order they were written, so element i of
		// the response usually corresponds to element i of the reference. See
		// the limitation on this function for what a mid-list edit costs.
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
	referenceValues := make(map[string]*yaml.Node, len(reference.Content)/2)
	for i := 0; i+1 < len(reference.Content); i += 2 {
		key := reference.Content[i].Value
		if _, duplicate := rank[key]; duplicate {
			continue
		}
		rank[key] = i
		referenceValues[key] = reference.Content[i+1]
	}

	type entry struct {
		key   *yaml.Node
		value *yaml.Node
		rank  int
	}

	entries := make([]entry, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		alignKeyOrder(value, referenceValues[key.Value])

		keyRank, known := rank[key.Value]
		if !known {
			keyRank = len(reference.Content) + i
		}
		entries = append(entries, entry{key: key, value: value, rank: keyRank})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].rank < entries[j].rank })

	node.Content = node.Content[:0]
	for _, e := range entries {
		node.Content = append(node.Content, e.key, e.value)
	}
}
