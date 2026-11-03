package converter

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

const customSkillAdminRole = "admin"

// customSkillAlwaysIgnoredFields are fields of a custom skill the server sets
// or derives, so they never express user intent. metadata.name is derived
// from the SKILL.md frontmatter.
var customSkillAlwaysIgnoredFields = []string{
	"metadata.name",
	"spec.permittedActions",
	"spec.source",
}

// CustomSkillYAMLEquivalent is ResourceYAMLEquivalent for Agent0 custom
// skills. yamlA is the reference (the user's config) and yamlB the value to
// compare against (state or the API response).
//
// When yamlA declares spec.permissions, the grants in yamlB are aligned with
// it before comparing: the admin grant the server always adds is ignored
// unless yamlA declares an admin entry, an admin entry is compared only by its
// defaultEnabled (the server fixes the admin actions), and defaultEnabled is
// ignored on every entry that yamlA declares without one. When yamlA has no
// spec.permissions, callers ignore the field through
// ConditionallyIgnoredFields.
func CustomSkillYAMLEquivalent(yamlA, yamlB string, additionalIgnoredFields []string) (bool, error) {
	alignedB, err := alignCustomSkillPermissions(yamlA, yamlB)
	if err != nil {
		return false, err
	}
	ignored := slices.Concat(additionalIgnoredFields, customSkillAlwaysIgnoredFields)
	return ResourceYAMLEquivalent(yamlA, alignedB, ignored, nil)
}

func alignCustomSkillPermissions(yamlA, yamlB string) (string, error) {
	desired, ok, err := customSkillPermissions(yamlA)
	if err != nil {
		return "", fmt.Errorf("error parsing first custom skill yaml: %w", err)
	}
	if !ok {
		return yamlB, nil
	}

	var docB map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlB), &docB); err != nil {
		return "", fmt.Errorf("error parsing second custom skill yaml: %w", err)
	}
	spec, _ := docB["spec"].(map[string]interface{})
	if spec == nil {
		return yamlB, nil
	}
	actual, _ := spec["permissions"].([]interface{})

	desiredByPrincipal := make(map[string]map[string]interface{}, len(desired))
	for _, entry := range desired {
		if grant, ok := entry.(map[string]interface{}); ok {
			desiredByPrincipal[customSkillPrincipal(grant)] = grant
		}
	}
	desiredAdmin, declaresAdmin := desiredByPrincipal["role:"+customSkillAdminRole]

	aligned := make([]interface{}, 0, len(actual))
	sawAdmin := false
	for _, entry := range actual {
		grant, ok := entry.(map[string]interface{})
		if !ok {
			aligned = append(aligned, entry)
			continue
		}
		principal := customSkillPrincipal(grant)
		isAdmin := principal == "role:"+customSkillAdminRole
		if isAdmin && !declaresAdmin {
			continue
		}
		if isAdmin {
			sawAdmin = true
		}
		if want, declared := desiredByPrincipal[principal]; declared {
			grant = alignCustomSkillGrant(want, grant, isAdmin)
		}
		aligned = append(aligned, grant)
	}

	// The admin grant is left out of a stripped document when it is on by
	// default, so a missing admin grant means defaultEnabled: true.
	if declaresAdmin && !sawAdmin {
		admin := alignCustomSkillGrant(desiredAdmin, map[string]interface{}{
			"role":           customSkillAdminRole,
			"defaultEnabled": true,
		}, true)
		aligned = append(aligned, admin)
	}

	spec["permissions"] = aligned
	out, err := yaml.Marshal(docB)
	if err != nil {
		return "", fmt.Errorf("error serializing custom skill yaml: %w", err)
	}
	return string(out), nil
}

// alignCustomSkillGrant returns a copy of actual with the fields that want
// does not constrain taken from want.
func alignCustomSkillGrant(want, actual map[string]interface{}, isAdmin bool) map[string]interface{} {
	out := make(map[string]interface{}, len(actual))
	for k, v := range actual {
		out[k] = v
	}
	if _, set := want["defaultEnabled"]; !set {
		delete(out, "defaultEnabled")
	}
	if isAdmin {
		if actions, set := want["actions"]; set {
			out["actions"] = actions
		} else {
			delete(out, "actions")
		}
	}
	return out
}

// customSkillPermissions returns spec.permissions of a custom skill document
// and whether the document declares it.
func customSkillPermissions(yamlStr string) ([]interface{}, bool, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &doc); err != nil {
		return nil, false, err
	}
	spec, _ := doc["spec"].(map[string]interface{})
	if spec == nil {
		return nil, false, nil
	}
	raw, present := spec["permissions"]
	if !present || raw == nil {
		return nil, false, nil
	}
	permissions, _ := raw.([]interface{})
	return permissions, true, nil
}

// customSkillPrincipal identifies who a grant applies to. Each grant names
// exactly one of a role, a team or a user.
func customSkillPrincipal(grant map[string]interface{}) string {
	for _, key := range []string{"role", "teamId", "userId"} {
		if v, ok := grant[key].(string); ok && v != "" {
			return key + ":" + v
		}
	}
	return ""
}
