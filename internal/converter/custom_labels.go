package converter

import (
	"fmt"
	"maps"

	"gopkg.in/yaml.v3"
)

// CustomLabelsYAMLEquivalent is ResourceYAMLEquivalent for assets whose API
// stores the user's labels under metadata.labels.custom, also comparing those
// labels.
//
// ResourceYAMLEquivalent strips the whole metadata.labels subtree, because the
// other keys in it are managed by Dash0. On its own it therefore cannot see a
// custom label being added, changed or removed, either in the configuration or
// outside Terraform. The custom map is compared here by its string values, and
// an absent map is equivalent to an empty one.
//
// Use it only for assets whose API returns metadata.labels.custom on read.
// For an asset that drops the map, it would report a change on every plan.
func CustomLabelsYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields []string) (bool, error) {
	equivalent, err := ResourceYAMLEquivalent(yamlA, yamlB, additionalIgnoredFields, nil)
	if err != nil || !equivalent {
		return equivalent, err
	}

	customA, err := customLabels(yamlA)
	if err != nil {
		return false, fmt.Errorf("error reading first custom labels: %w", err)
	}

	customB, err := customLabels(yamlB)
	if err != nil {
		return false, fmt.Errorf("error reading second custom labels: %w", err)
	}

	return maps.Equal(customA, customB), nil
}

// customLabels returns metadata.labels.custom of a document as strings. A
// missing or empty map yields an empty map.
func customLabels(yamlStr string) (map[string]string, error) {
	var document struct {
		Metadata struct {
			Labels struct {
				Custom map[string]interface{} `yaml:"custom"`
			} `yaml:"labels"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &document); err != nil {
		return nil, err
	}

	custom := make(map[string]string, len(document.Metadata.Labels.Custom))
	for key, value := range document.Metadata.Labels.Custom {
		if value == nil {
			continue
		}
		custom[key] = fmt.Sprintf("%v", value)
	}
	return custom, nil
}
