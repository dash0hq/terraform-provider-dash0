package provider

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

// JSON marshaling - outputs duration string
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// YAML marshaling - outputs duration string
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// For JSON
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	return d.unmarshalValue(v)
}

// For YAML
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var v interface{}
	if err := value.Decode(&v); err != nil {
		return err
	}
	return d.unmarshalValue(v)
}

// Shared logic
func (d *Duration) unmarshalValue(v interface{}) error {
	switch value := v.(type) {
	case string:
		duration, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(duration)
	case int:
		*d = Duration(time.Duration(value))
	case float64:
		*d = Duration(time.Duration(value))
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
	return nil
}

type Dash0CheckRule struct {
	Dataset       string                   `json:"dataset"`
	ID            string                   `json:"id,omitempty"`
	Name          string                   `json:"name"`
	Expression    string                   `json:"expression"`
	Thresholds    Dash0CheckRuleThresholds `json:"thresholds"`
	Summary       string                   `json:"summary"`
	Description   string                   `json:"description"`
	Interval      Duration                 `json:"interval,omitempty"`
	For           Duration                 `json:"for,omitempty"`
	KeepFiringFor Duration                 `json:"keepFiringFor,omitempty"`
	Labels        map[string]string        `json:"labels"`
	Annotations   map[string]string        `json:"annotations"`
	Enabled       bool                     `json:"enabled"`
}

type Dash0CheckRuleThresholds struct {
	Degraded int `json:"degraded"`
	Failed   int `json:"failed"`
}

type PrometheusRules struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Metadata   map[string]string   `json:"metadata" yaml:"metadata"`
	Spec       PrometheusRulesSpec `json:"spec" yaml:"spec"`
}

type PrometheusRulesSpec struct {
	Groups []PrometheusRulesGroup `json:"groups" yaml:"groups"`
}

type PrometheusRulesGroup struct {
	Name     string           `json:"name" yaml:"name"`
	Interval Duration         `json:"interval" yaml:"interval"`
	Rules    []PrometheusRule `json:"rules" yaml:"rules"`
}

type PrometheusRule struct {
	Alert         string            `json:"alert" yaml:"alert"`
	Expr          string            `json:"expr" yaml:"expr"`
	For           Duration          `json:"for" yaml:"for"`
	KeepFiringFor Duration          `json:"keep_firing_for,omitempty" yaml:"keep_firing_for,omitempty"`
	Annotations   map[string]string `json:"annotations" yaml:"annotations"`
	Labels        map[string]string `json:"labels" yaml:"labels"`
}
