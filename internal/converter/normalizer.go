package converter

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
	"metadata.annotations",
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

	// Remove ignored fields and empty values
	cleanupMap(parsedYaml, ignoredFields)

	// Create a new encoder with consistent settings
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2) // Use consistent 2-space indentation

	if err := encoder.Encode(parsedYaml); err != nil {
		return "", fmt.Errorf("error encoding YAML: %w", err)
	}

	err := encoder.Close()
	if err != nil {
		return "", fmt.Errorf("error closing YAML encoder: %w", err)
	}

	// Remove the trailing newline that yaml.Marshal adds
	result := strings.TrimSuffix(buf.String(), "\n")

	return string(result), nil
}

// removeZeroThresholdAnnotations removes dash0-threshold-critical and dash0-threshold-degraded
// annotations when their value is "0". This treats zero-value thresholds as semantically
// equivalent to not having the annotation, ensuring consistent comparison regardless of
// whether the user includes them in their config.
func removeZeroThresholdAnnotations(annotations map[string]interface{}) {
	for key, value := range annotations {
		if key == "dash0-threshold-critical" || key == "dash0-threshold-degraded" {
			if strVal, ok := value.(string); ok && strVal == "0" {
				delete(annotations, key)
			}
		}
	}
}

// cleanupMap removes specified fields by path and empty values from a map in place.
// fieldsToRemove contains dot-separated paths (e.g., "metadata.createdAt").
// Empty arrays, maps, and strings are also removed to ensure consistent comparison.
func cleanupMap(data map[string]interface{}, fieldsToRemove []string) {
	// Build maps for what to remove at this level vs what to recurse into
	removeHere := make(map[string]bool)
	nestedRemovals := make(map[string][]string)
	for _, path := range fieldsToRemove {
		if idx := strings.Index(path, "."); idx == -1 {
			removeHere[path] = true
		} else {
			key := path[:idx]
			nestedRemovals[key] = append(nestedRemovals[key], path[idx+1:])
		}
	}

	for key, value := range data {
		if removeHere[key] {
			delete(data, key)
			continue
		}

		switch v := value.(type) {
		case map[string]interface{}:
			cleanupMap(v, nestedRemovals[key])
			// Remove zero-value threshold annotations for semantic equivalence
			if key == "annotations" {
				removeZeroThresholdAnnotations(v)
			}
			if isEmpty(v) {
				delete(data, key)
			}
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					cleanupMap(m, nil)
				}
			}
			if len(v) == 0 {
				delete(data, key)
			}
		case string:
			if v == "" {
				delete(data, key)
			}
		}
	}
}

// isEmpty checks if a map is empty or contains only empty values
func isEmpty(m map[string]interface{}) bool {
	if len(m) == 0 {
		return true
	}
	for _, value := range m {
		switch v := value.(type) {
		case map[string]interface{}:
			if !isEmpty(v) {
				return false
			}
		case []interface{}:
			if len(v) > 0 {
				return false
			}
		case string:
			if v != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
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
