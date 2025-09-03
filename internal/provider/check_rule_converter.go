package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func convertDash0JSONtoPrometheusRules(dash0CheckRuleJson string) (*PrometheusRules, error) {
	var dash0CheckRule Dash0CheckRule
	if err := json.Unmarshal([]byte(dash0CheckRuleJson), &dash0CheckRule); err != nil {
		return nil, fmt.Errorf("error parsing resource JSON: %w", err)
	}

	nameParts := strings.SplitN(dash0CheckRule.Name, " - ", 2)
	var groupName string
	var alertName string
	if len(nameParts) == 2 {
		groupName = nameParts[0]
		alertName = nameParts[1]
	} else {
		groupName = dash0CheckRule.Name
		alertName = dash0CheckRule.Name
	}

	promRule := PrometheusRule{
		Alert:         alertName,
		Expr:          dash0CheckRule.Expression,
		For:           dash0CheckRule.For,
		KeepFiringFor: dash0CheckRule.KeepFiringFor,
		Labels:        dash0CheckRule.Labels,
		Annotations:   dash0CheckRule.Annotations,
	}

	if dash0CheckRule.Summary != "" {
		promRule.Annotations["summary"] = dash0CheckRule.Summary
	}
	if dash0CheckRule.Description != "" {
		promRule.Annotations["description"] = dash0CheckRule.Description
	}
	if dash0CheckRule.Thresholds.Failed != 0 {
		promRule.Annotations["dash0-threshold-critical"] = strconv.Itoa(dash0CheckRule.Thresholds.Failed)
	}
	if dash0CheckRule.Thresholds.Degraded != 0 {
		promRule.Annotations["dash0-threshold-degraded"] = strconv.Itoa(dash0CheckRule.Thresholds.Degraded)
	}

	promRules := &PrometheusRules{
		APIVersion: "monitoring.coreos.com/v1",
		Kind:       "PrometheusRule",
		Metadata:   map[string]string{},
		Spec: PrometheusRulesSpec{
			Groups: []PrometheusRulesGroup{
				{
					Name:     groupName,
					Interval: dash0CheckRule.Interval,
					Rules:    []PrometheusRule{promRule},
				},
			},
		},
	}
	return promRules, nil
}

func convertPromYAMLToDash0CheckRule(promRuleYaml string, dataset string) (*Dash0CheckRule, error) {
	var promRule PrometheusRules
	if err := yaml.Unmarshal([]byte(promRuleYaml), &promRule); err != nil {
		return nil, fmt.Errorf("error parsing resource YAML: %w", err)
	}

	if len(promRule.Spec.Groups) != 1 {
		return nil, fmt.Errorf("currently only one group is supported")
	}
	group := promRule.Spec.Groups[0]

	if len(promRule.Spec.Groups[0].Rules) != 1 {
		return nil, fmt.Errorf("currently only one rule per group is supported")
	}
	rule := group.Rules[0]

	name := fmt.Sprintf("%s - %s", group.Name, rule.Alert)
	dash0CheckRule := &Dash0CheckRule{
		Name:          name,
		Interval:      group.Interval,
		Annotations:   rule.Annotations,
		Labels:        rule.Labels,
		For:           rule.For,
		Expression:    rule.Expr,
		KeepFiringFor: rule.KeepFiringFor,
		Thresholds:    Dash0CheckRuleThresholds{},
		Enabled:       true,
		Dataset:       dataset,
	}

	if summary, ok := rule.Annotations["summary"]; ok {
		dash0CheckRule.Summary = summary
	}
	if description, ok := rule.Annotations["description"]; ok {
		dash0CheckRule.Description = description
	}
	if thresholdCritial, ok := rule.Annotations["dash0-threshold-critical"]; ok {
		if criticalInt, err := strconv.Atoi(thresholdCritial); err == nil {
			dash0CheckRule.Thresholds.Failed = criticalInt
			delete(dash0CheckRule.Annotations, "dash0-threshold-critical")
		} else {
			return nil, fmt.Errorf("invalid value for dash0-threshold-critical: %v", err)
		}
	}
	if thresholdDegraded, ok := rule.Annotations["dash0-threshold-degraded"]; ok {
		if degradedInt, err := strconv.Atoi(thresholdDegraded); err == nil {
			dash0CheckRule.Thresholds.Degraded = degradedInt
			delete(dash0CheckRule.Annotations, "dash0-threshold-degraded")
		} else {
			return nil, fmt.Errorf("invalid value for dash0-threshold-degraded: %v", err)
		}
	}

	return dash0CheckRule, nil
}
