package converter

import (
	"fmt"
	"strings"
	"time"

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

// stringifyMapValues converts all non-string values in a map to their string
// representation. This is used for annotation maps which are semantically
// map[string]string, but untyped YAML parsing may produce non-string types
// (e.g., an unquoted 5000 becomes int, true becomes bool).
func stringifyMapValues(m map[string]interface{}) {
	for key, value := range m {
		if _, ok := value.(string); !ok {
			m[key] = fmt.Sprintf("%v", value)
		}
	}
}

// removeDefaultAnnotationValues removes annotations whose values match the defaults
// used by the check rule round-trip conversion. This ensures that explicitly setting
// a default value is treated as semantically equivalent to omitting the annotation.
//   - dash0-threshold-critical: "0" and dash0-threshold-degraded: "0" are removed
//     because zero-value thresholds are omitted during the Dash0 JSON → Prometheus YAML conversion.
//   - dash0-enabled: "true" is removed because true is the default and is omitted
//     during the Dash0 JSON → Prometheus YAML conversion (see check_rule.go).
func removeDefaultAnnotationValues(annotations map[string]interface{}) {
	for key, value := range annotations {
		strVal, ok := value.(string)
		if !ok {
			continue
		}
		if (key == "dash0-threshold-critical" || key == "dash0-threshold-degraded") && strVal == "0" {
			delete(annotations, key)
		}
		if key == "dash0-enabled" && strVal == "true" {
			delete(annotations, key)
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
			if key == "annotations" || key == "labels" {
				// Annotations and labels are semantically map[string]string, but untyped
				// YAML parsing may produce non-string types (e.g., unquoted 5000 becomes
				// int, unquoted true becomes bool). Stringify all values so comparison
				// matches the round-tripped form.
				stringifyMapValues(v)
			}
			if key == "annotations" {
				// Remove annotations with default values for semantic equivalence.
				// IMPORTANT: Must be called after stringifyMapValues since it expects string values.
				removeDefaultAnnotationValues(v)
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
			} else if key == "keep_firing_for" {
				// keep_firing_for uses Duration with omitempty, so yaml.Marshal drops
				// it when the value is zero. Remove it here so "keep_firing_for: 0s"
				// in user YAML matches the round-tripped YAML that omits the field.
				// If parsing fails, the value is not a duration, so keep it as-is.
				if d, err := time.ParseDuration(v); err == nil && d == 0 {
					delete(data, key)
				}
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

// normalizeNumericTypes recursively converts all integer and float types to float64
// in a parsed YAML/JSON structure. This ensures consistent comparison when the same
// numeric value appears as different types (e.g., int in YAML and float64 in JSON).
func normalizeNumericTypes(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			val[k] = normalizeNumericTypes(v)
		}
		return val
	case []interface{}:
		for i, v := range val {
			val[i] = normalizeNumericTypes(v)
		}
		return val
	case int:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case float32:
		return float64(val)
	default:
		return v
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

	// Normalize numeric types (int -> float64) to handle YAML vs JSON type differences
	parsedA = normalizeNumericTypes(parsedA)
	parsedB = normalizeNumericTypes(parsedB)

	cmpOptions := []cmp.Option{
		// Ignore order of slices deeper in the structure
		cmpopts.SortSlices(func(x, y interface{}) bool {
			return fmt.Sprint(x) < fmt.Sprint(y)
		}),
		// Duration-aware string comparison: treats "2m" and "2m0s" as equivalent
		// when both strings are valid Go duration strings
		cmp.FilterValues(
			func(x, y string) bool {
				_, errX := time.ParseDuration(x)
				_, errY := time.ParseDuration(y)
				return errX == nil && errY == nil
			},
			cmp.Comparer(func(x, y string) bool {
				dx, _ := time.ParseDuration(x)
				dy, _ := time.ParseDuration(y)
				return dx == dy
			}),
		),
	}
	// Compare the parsed structures
	return cmp.Equal(parsedA, parsedB, cmpOptions...), nil
}
