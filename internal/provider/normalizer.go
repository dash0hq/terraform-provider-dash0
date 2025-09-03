package provider

import (
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gopkg.in/yaml.v3"
)

// Fields to ignore when comparing resource YAMLs
var ignoredFields = []string{
	"apiVersion",
	"kind",
	"metadata.labels",
	"metadata.createdAt",
	"metadata.updatedAt",
	"metadata.version",
	"metadata.dash0Extensions",
	"metadata.name",
}

// NormalizeYAML normalizes a YAML by removing the fields we want to ignore
// when comparing for drift detection.
func NormalizeYAML(yamlStr string) (string, error) {
	// Parse YAML into an interface
	var parsedYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &parsedYaml); err != nil {
		return "", fmt.Errorf("error parsing resource YAML: %w", err)
	}

	// Remove ignored fields
	for _, field := range ignoredFields {
		removeField(parsedYaml, field)
	}

	// Create a new encoder with consistent settings
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2) // Use consistent 2-space indentation

	if err := encoder.Encode(parsedYaml); err != nil {
		return "", fmt.Errorf("error encoding YAML: %w", err)
	}
	encoder.Close()

	// Remove the trailing newline that yaml.Marshal adds
	result := strings.TrimSuffix(buf.String(), "\n")

	return string(result), nil
}

// removeField removes a field from a map by path (e.g., "metadata.createdAt")
func removeField(data map[string]interface{}, path string) {
	// Split the path into parts
	parts := []string{}
	current := ""
	for _, c := range path {
		if c == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	// Navigate the path
	var currentMap interface{} = data
	for i, part := range parts {
		if i == len(parts)-1 {
			// Last part - delete the field
			if m, ok := currentMap.(map[string]interface{}); ok {
				delete(m, part)
			} else if m, ok := currentMap.(map[interface{}]interface{}); ok {
				delete(m, part)
			}
			return
		}

		// Navigate to the next level
		var next interface{}
		if m, ok := currentMap.(map[string]interface{}); ok {
			next = m[part]
		} else if m, ok := currentMap.(map[interface{}]interface{}); ok {
			next = m[part]
		} else {
			// Can't navigate further
			return
		}

		// Check if we can continue
		if next == nil {
			return
		}

		// Continue with the next part
		currentMap = next
	}
}

// ResourceYAMLEquivalent checks if two resource YAMLs are equivalent,
// ignoring fields we don't care about for drift detection
func ResourceYAMLEquivalent(yamlA, yamlB string) (bool, error) {
	// Normalize both YAMLs
	normalizedA, err := NormalizeYAML(yamlA)
	if err != nil {
		return false, fmt.Errorf("error normalizing first resource yaml: %w", err)
	}

	normalizedB, err := NormalizeYAML(yamlB)
	if err != nil {
		return false, fmt.Errorf("error normalizing second resource yaml: %w", err)
	}

	// Parse both normalized YAMLs into interfaces
	var parsedA, parsedB interface{}
	if err := yaml.Unmarshal([]byte(normalizedA), &parsedA); err != nil {
		return false, fmt.Errorf("error parsing first normalized resource yaml: %w", err)
	}
	if err := yaml.Unmarshal([]byte(normalizedB), &parsedB); err != nil {
		return false, fmt.Errorf("error parsing second normalized resource yaml: %w", err)
	}

	// make sure that the order of slices deeper in the structure does not matter
	cmpOptions := []cmp.Option{cmpopts.SortSlices(func(x, y interface{}) bool {
		return fmt.Sprint(x) < fmt.Sprint(y)
	})}
	// Compare the parsed structures
	return cmp.Equal(parsedA, parsedB, cmpOptions...), nil
}
