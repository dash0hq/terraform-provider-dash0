package converter

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gopkg.in/yaml.v3"
)

// ignoredFields are always removed when comparing resource YAMLs.
var ignoredFields = []string{
	"apiVersion",
	"kind",
	"metadata.labels",
	"metadata.annotations",
	"metadata.createdAt",
	"metadata.updatedAt",
	"metadata.version",
	"metadata.dash0Extensions",
}

// ConditionallyIgnoredFields are fields ignored during comparison only when
// absent from the reference YAML (typically the user's config). These are
// fields the API enriches on retrieval but that users may optionally manage.
// When present in the user's config, drift detection is preserved.
var ConditionallyIgnoredFields = []string{
	"spec.permissions", // API-managed: stored separately, enriched on retrieval
}

// NormalizeYAML normalizes a YAML by removing the fields we want to ignore
// when comparing for drift detection. Additional fields to ignore can be
// passed via additionalIgnoredFields (e.g., from ConditionallyIgnoredFields).
func NormalizeYAML(yamlStr string, additionalIgnoredFields ...string) (string, error) {
	// Parse YAML into an interface
	var parsedYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &parsedYaml); err != nil {
		return "", fmt.Errorf("error parsing resource YAML: %w", err)
	}

	// Merge always-ignored fields with any additional fields
	allIgnored := ignoredFields
	if len(additionalIgnoredFields) > 0 {
		allIgnored = make([]string, 0, len(ignoredFields)+len(additionalIgnoredFields))
		allIgnored = append(allIgnored, ignoredFields...)
		allIgnored = append(allIgnored, additionalIgnoredFields...)
	}

	// Remove ignored fields and empty values
	cleanupMap(parsedYaml, allIgnored)

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

		// JSON null becomes Go nil after yaml.Unmarshal; treat as absent.
		if value == nil {
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
			} else if key == "for" || key == "keep_firing_for" {
				// for and keep_firing_for use Duration with omitempty, so yaml.Marshal
				// drops them when the value is zero. Remove them here so "for: 0s" /
				// "keep_firing_for: 0s" in user YAML matches the round-tripped YAML
				// that omits the field.
				// If parsing fails, the value is not a duration, so keep it as-is.
				if d, err := time.ParseDuration(v); err == nil && d == 0 {
					delete(data, key)
				}
			}
		}
	}
}

// isEmpty checks if a map is empty or contains only empty/nil values
func isEmpty(m map[string]interface{}) bool {
	if len(m) == 0 {
		return true
	}
	for _, value := range m {
		if value == nil {
			continue
		}
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

// canonicalString produces a deterministic string representation of a value,
// recursively sorting maps by key and slices by their canonical representation.
// This ensures that the SortSlices comparator produces stable sort keys even
// when nested structures (like action lists within permissions) differ in order.
func canonicalString(v interface{}) string {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+":"+canonicalString(val[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []interface{}:
		strs := make([]string, len(val))
		for i, item := range val {
			strs[i] = canonicalString(item)
		}
		sort.Strings(strs)
		return "[" + strings.Join(strs, ",") + "]"
	default:
		return fmt.Sprint(v)
	}
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
// ignoring fields we don't care about for drift detection.
// yamlA is the reference (typically the user's config) and yamlB is the
// value to compare against (typically the API response).
// Additional fields to ignore can be passed via additionalIgnoredFields
// (e.g., conditionally ignored fields that are absent from the user's config).
func ResourceYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields ...string) (bool, error) {
	// Normalize both YAMLs
	normalizedA, err := NormalizeYAML(yamlA, additionalIgnoredFields...)
	if err != nil {
		return false, fmt.Errorf("error normalizing first resource yaml: %w", err)
	}

	normalizedB, err := NormalizeYAML(yamlB, additionalIgnoredFields...)
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

	// Strip zero-value fields from B that are absent in A. This prevents
	// API-enriched defaults (e.g., "enabled": false, "retries": null) from
	// being treated as drift when the user didn't set those fields. If the
	// user explicitly set a zero value, it will be present in A and preserved.
	if mapA, ok := parsedA.(map[string]interface{}); ok {
		if mapB, ok := parsedB.(map[string]interface{}); ok {
			stripAbsentZeroValues(mapA, mapB)
		}
	}

	cmpOptions := []cmp.Option{
		// Ignore order of slices deeper in the structure.
		// Uses canonicalString which recursively sorts nested structures so that
		// the sort key is stable regardless of inner element ordering (e.g., if
		// the API returns list items in a different order than the user's config).
		cmpopts.SortSlices(func(x, y interface{}) bool {
			return canonicalString(x) < canonicalString(y)
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

// stripAbsentZeroValues removes keys from target that don't exist in reference
// and have zero values (false, 0, empty string, empty map, empty slice, nil).
// This is applied recursively to nested maps.
func stripAbsentZeroValues(reference, target map[string]interface{}) {
	for key, targetVal := range target {
		refVal, existsInRef := reference[key]
		if !existsInRef {
			// Key is in target but not in reference — strip if zero value
			if isZeroValue(targetVal) {
				delete(target, key)
			}
			continue
		}
		// Both have the key — recurse into nested maps
		if refMap, ok := refVal.(map[string]interface{}); ok {
			if targetMap, ok := targetVal.(map[string]interface{}); ok {
				stripAbsentZeroValues(refMap, targetMap)
				if len(targetMap) == 0 {
					delete(target, key)
				}
			}
		}
	}
}

// isZeroValue returns true if v is a JSON zero value (false, 0, "", nil,
// empty map, or empty slice).
func isZeroValue(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case bool:
		return !val
	case float64:
		return val == 0
	case int:
		return val == 0
	case string:
		return val == ""
	case map[string]interface{}:
		return isEmpty(val)
	case []interface{}:
		return len(val) == 0
	default:
		return false
	}
}

// FieldsAbsentFromYAML returns which of the given dot-separated field paths
// are absent from the parsed YAML. Used to conditionally ignore API-managed
// fields that the user didn't include in their config.
func FieldsAbsentFromYAML(yamlStr string, fields []string) []string {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &parsed); err != nil {
		return nil // on error, don't ignore anything extra (safe default)
	}

	var absent []string
	for _, field := range fields {
		if !hasFieldPath(parsed, field) {
			absent = append(absent, field)
		}
	}
	return absent
}

// hasFieldPath checks if a dot-separated path exists in a nested map.
func hasFieldPath(data map[string]interface{}, path string) bool {
	parts := strings.SplitN(path, ".", 2)
	val, exists := data[parts[0]]
	if !exists || val == nil {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	nested, ok := val.(map[string]interface{})
	if !ok {
		return false
	}
	return hasFieldPath(nested, parts[1])
}
