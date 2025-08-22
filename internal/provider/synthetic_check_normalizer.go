package provider

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// Fields to ignore when comparing synthetic checks
var syntheticCheckIgnoredFields = []string{
	"apiVersion",
	"kind",
	"metadata.labels",
	"metadata.createdAt",
	"metadata.updatedAt",
	"metadata.version",
	"metadata.dash0Extensions",
}

// NormalizeSyntheticCheckYAML normalizes a synthetic check YAML by removing the fields we want to ignore
// when comparing for drift detection.
func NormalizeSyntheticCheckYAML(yamlStr string) (string, error) {
	// Parse YAML into an interface
	var parsedYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &parsedYaml); err != nil {
		return "", fmt.Errorf("error parsing synthetic check YAML: %w", err)
	}

	// Remove ignored fields
	for _, field := range syntheticCheckIgnoredFields {
		removeSyntheticCheckField(parsedYaml, field)
	}

	// Marshal back to YAML
	normalizedYaml, err := yaml.Marshal(parsedYaml)
	if err != nil {
		return "", fmt.Errorf("error marshalling normalized synthetic check YAML: %w", err)
	}

	return string(normalizedYaml), nil
}

// removeSyntheticCheckField removes a field from a map by path (e.g., "metadata.createdAt")
func removeSyntheticCheckField(data map[string]interface{}, path string) {
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

		if next == nil {
			// Path doesn't exist
			return
		}

		currentMap = next
	}
}

// SyntheticChecksEquivalent compares two synthetic check YAML strings, ignoring certain fields
func SyntheticChecksEquivalent(yaml1, yaml2 string) (bool, error) {
	// Normalize both YAMLs
	normalized1, err := NormalizeSyntheticCheckYAML(yaml1)
	if err != nil {
		return false, fmt.Errorf("error normalizing first synthetic check: %w", err)
	}

	normalized2, err := NormalizeSyntheticCheckYAML(yaml2)
	if err != nil {
		return false, fmt.Errorf("error normalizing second synthetic check: %w", err)
	}

	// Parse normalized YAMLs
	var parsed1, parsed2 interface{}
	if err := yaml.Unmarshal([]byte(normalized1), &parsed1); err != nil {
		return false, fmt.Errorf("error parsing first normalized synthetic check: %w", err)
	}
	if err := yaml.Unmarshal([]byte(normalized2), &parsed2); err != nil {
		return false, fmt.Errorf("error parsing second normalized synthetic check: %w", err)
	}

	// Compare using deep equal
	return reflect.DeepEqual(parsed1, parsed2), nil
}
