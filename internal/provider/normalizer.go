package provider

import (
	"fmt"
	"reflect"

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

	// Marshal back to YAML
	normalizedYaml, err := yaml.Marshal(parsedYaml)
	if err != nil {
		return "", fmt.Errorf("error marshalling normalized resource YAML: %w", err)
	}

	return string(normalizedYaml), nil
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

	// Compare the parsed structures
	return reflect.DeepEqual(parsedA, parsedB), nil
}
