package provider

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ConvertYAMLToJSON converts a YAML string to a JSON string
func ConvertYAMLToJSON(yamlString string) (string, error) {
	// Parse YAML into an interface{}
	var yamlObj interface{}
	err := yaml.Unmarshal([]byte(yamlString), &yamlObj)
	if err != nil {
		return "", fmt.Errorf("error parsing YAML: %w", err)
	}

	// Marshal the interface{} into JSON
	jsonBytes, err := json.Marshal(yamlObj)
	if err != nil {
		return "", fmt.Errorf("error marshaling to JSON: %w", err)
	}

	return string(jsonBytes), nil
}