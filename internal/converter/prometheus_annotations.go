package converter

import (
	"fmt"

	"gopkg.in/yaml.v3"

	dash0yaml "github.com/dash0hq/dash0-api-client-go/yaml"
)

// MoveTopLevelAnnotationsIntoRules returns a copy of yamlStr with a
// PrometheusRule document's top-level metadata.annotations merged into every
// rule's own annotations, rule-level annotations winning on key conflict, and
// the now-redundant top-level map removed.
//
// This mirrors dash0-api-client-go's mergeAnnotations
// (dash0hq/dash0-api-client-go#29), so a config declaring
// metadata.annotations is comparable to the API response, which already
// reflects the merge and carries no top-level annotations of its own.
//
// The top-level map is removed rather than left in place because
// normalization preserves selected metadata annotations (dash0.com/sharing).
// Leaving it would keep such a key on both sides of a comparison in two
// different shapes, so the two would never compare equal.
//
// Intended for comparison copies only: it changes where annotations live, so
// the result must not be written back to state.
//
// Best-effort. Returns the input unchanged if it cannot be parsed, is not
// shaped like a PrometheusRule, or has no top-level annotations to move.
func MoveTopLevelAnnotationsIntoRules(yamlStr string) string {
	var doc map[string]interface{}
	if yaml.Unmarshal([]byte(yamlStr), &doc) != nil {
		return yamlStr
	}

	metadata, _ := doc["metadata"].(map[string]interface{})
	if metadata == nil {
		return yamlStr
	}
	topLevelAnnotations, _ := metadata["annotations"].(map[string]interface{})
	if len(topLevelAnnotations) == 0 {
		return yamlStr
	}

	spec, _ := doc["spec"].(map[string]interface{})
	if spec == nil {
		return yamlStr
	}
	groups, _ := spec["groups"].([]interface{})

	changed := false
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		if group == nil {
			continue
		}
		rules, _ := group["rules"].([]interface{})
		for _, r := range rules {
			rule, _ := r.(map[string]interface{})
			if rule == nil {
				continue
			}
			ruleAnnotations, _ := rule["annotations"].(map[string]interface{})
			merged := dash0yaml.MergeAnnotations(
				toStringMap(topLevelAnnotations),
				toStringMap(ruleAnnotations),
			)
			rule["annotations"] = merged
			changed = true
		}
	}
	if !changed {
		return yamlStr
	}

	delete(metadata, "annotations")

	out, err := yaml.Marshal(doc)
	if err != nil {
		return yamlStr
	}
	return string(out)
}

// toStringMap renders a YAML annotation map as map[string]string so it can be
// merged by dash0yaml.MergeAnnotations. Annotation values are strings on the
// wire, and the API response this result is compared against always carries
// them as strings, so rendering a YAML scalar such as an unquoted 5000 to
// "5000" matches the shape the comparison already expects.
func toStringMap(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		// A YAML key with no value unmarshals to nil, and %v would render it
		// as the literal "<nil>", which no API response can match. An empty
		// annotation is an empty string.
		if v == nil {
			out[k] = ""
			continue
		}
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}
