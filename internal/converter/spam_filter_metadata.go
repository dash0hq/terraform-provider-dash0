package converter

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// SpamFilterYAMLEquivalent is ResourceYAMLEquivalent for spam filters, also
// comparing whether each side leaves the filter enabled.
//
// ResourceYAMLEquivalent strips every metadata annotation, so on its own it
// cannot see a filter being switched off through dash0.com/enabled. That
// annotation is compared here by its effective value: an absent or
// unparseable value counts as "true", which is the default Dash0 applies.
// Declaring "true" explicitly is therefore equivalent to omitting it.
func SpamFilterYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields []string) (bool, error) {
	equivalent, err := ResourceYAMLEquivalent(yamlA, yamlB, additionalIgnoredFields, nil)
	if err != nil || !equivalent {
		return equivalent, err
	}

	enabledA, err := spamFilterEnabled(yamlA)
	if err != nil {
		return false, fmt.Errorf("error reading first spam filter metadata: %w", err)
	}

	enabledB, err := spamFilterEnabled(yamlB)
	if err != nil {
		return false, fmt.Errorf("error reading second spam filter metadata: %w", err)
	}

	return enabledA == enabledB, nil
}

// spamFilterEnabled returns the effective value of the dash0.com/enabled
// annotation in a spam filter document.
func spamFilterEnabled(yamlStr string) (bool, error) {
	var document struct {
		Metadata struct {
			Annotations map[string]interface{} `yaml:"annotations"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &document); err != nil {
		return false, err
	}

	value, ok := document.Metadata.Annotations[AnnotationEnabled]
	if !ok || value == nil {
		return true, nil
	}
	enabled, err := strconv.ParseBool(fmt.Sprintf("%v", value))
	if err != nil {
		return true, nil
	}
	return enabled, nil
}
