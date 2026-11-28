package converter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TelemetryTransformationRuleYAMLEquivalent compares rule documents without reordering transformations, statements, or OTLP examples.
func TelemetryTransformationRuleYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields []string) (bool, error) {
	normalizedA, err := normalizeTelemetryTransformationRule(yamlA, additionalIgnoredFields)
	if err != nil {
		return false, fmt.Errorf("error normalizing first telemetry transformation rule YAML: %w", err)
	}
	normalizedB, err := normalizeTelemetryTransformationRule(yamlB, additionalIgnoredFields)
	if err != nil {
		return false, fmt.Errorf("error normalizing second telemetry transformation rule YAML: %w", err)
	}
	return bytes.Equal(normalizedA, normalizedB), nil
}

func normalizeTelemetryTransformationRule(document string, additionalIgnoredFields []string) ([]byte, error) {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		return nil, fmt.Errorf("error parsing resource YAML: %w", err)
	}
	if parsed == nil {
		return nil, fmt.Errorf("expected a telemetry transformation rule object")
	}

	for _, path := range append([]string{"metadata.labels", "metadata.annotations"}, additionalIgnoredFields...) {
		removeTelemetryTransformationRuleField(parsed, path)
	}

	if spec, ok := parsed["spec"].(map[string]interface{}); ok {
		switch spec["priority"] {
		case nil, 200, float64(200):
			delete(spec, "priority")
		}
		for _, field := range []string{"conditions", "examples"} {
			if value := spec[field]; value == nil {
				delete(spec, field)
			} else if list, ok := value.([]interface{}); ok && len(list) == 0 {
				delete(spec, field)
			}
		}
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("error encoding resource JSON: %w", err)
	}
	return normalized, nil
}

func removeTelemetryTransformationRuleField(data map[string]interface{}, path string) bool {
	field, remainder, nested := strings.Cut(path, ".")
	value, exists := data[field]
	if !exists {
		return false
	}
	if !nested {
		delete(data, field)
		return true
	}
	child, ok := value.(map[string]interface{})
	if !ok || !removeTelemetryTransformationRuleField(child, remainder) {
		return false
	}
	if len(child) == 0 {
		delete(data, field)
	}
	return true
}
