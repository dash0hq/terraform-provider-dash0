package converter

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	dash0MetadataKeyPrefix   = "dash0.com/"
	sloDisplayNameAnnotation = "dash0.com/display-name"
	sloEnabledAnnotation     = "dash0.com/enabled"
)

var storedDash0SLOAnnotations = []string{
	sloDisplayNameAnnotation,
	sloEnabledAnnotation,
	AnnotationFolderPath,
	AnnotationSharing,
}

// SLOYAMLEquivalent is ResourceYAMLEquivalent for SLOs, also comparing the labels and annotations that Dash0 stores for an SLO.
func SLOYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields []string) (bool, error) {
	equivalent, err := ResourceYAMLEquivalent(yamlA, yamlB, additionalIgnoredFields, nil)
	if err != nil || !equivalent {
		return equivalent, err
	}

	labelsA, annotationsA, err := storedSLOMetadata(yamlA)
	if err != nil {
		return false, fmt.Errorf("error reading first SLO metadata: %w", err)
	}

	labelsB, annotationsB, err := storedSLOMetadata(yamlB)
	if err != nil {
		return false, fmt.Errorf("error reading second SLO metadata: %w", err)
	}

	return maps.Equal(labelsA, labelsB) && maps.Equal(annotationsA, annotationsB), nil
}

func storedSLOMetadata(yamlStr string) (labels, annotations map[string]string, err error) {
	var document struct {
		Metadata struct {
			Labels      map[string]interface{} `yaml:"labels"`
			Annotations map[string]interface{} `yaml:"annotations"`
		} `yaml:"metadata"`
	}
	if err = yaml.Unmarshal([]byte(yamlStr), &document); err != nil {
		return nil, nil, err
	}

	return storedSLOLabels(document.Metadata.Labels), storedSLOAnnotations(document.Metadata.Annotations), nil
}

func storedSLOLabels(labels map[string]interface{}) map[string]string {
	return storedSLOMetadataValues(labels, isUserSLOMetadataKey)
}

func storedSLOAnnotations(annotations map[string]interface{}) map[string]string {
	stored := storedSLOMetadataValues(annotations, func(key string) bool {
		return isUserSLOMetadataKey(key) || slices.Contains(storedDash0SLOAnnotations, key)
	})

	if strings.TrimSpace(stored[sloDisplayNameAnnotation]) == "" {
		delete(stored, sloDisplayNameAnnotation)
	}

	if enabled, err := strconv.ParseBool(stored[sloEnabledAnnotation]); err != nil || enabled {
		delete(stored, sloEnabledAnnotation)
	} else {
		stored[sloEnabledAnnotation] = strconv.FormatBool(enabled)
	}

	return stored
}

func storedSLOMetadataValues(values map[string]interface{}, isStored func(key string) bool) map[string]string {
	stored := make(map[string]string, len(values))
	for key, value := range values {
		if value == nil || !isStored(key) {
			continue
		}
		if text := fmt.Sprintf("%v", value); text != "" {
			stored[key] = text
		}
	}
	return stored
}

func isUserSLOMetadataKey(key string) bool {
	return strings.TrimSpace(key) != "" && !strings.HasPrefix(strings.ToLower(key), dash0MetadataKeyPrefix)
}
