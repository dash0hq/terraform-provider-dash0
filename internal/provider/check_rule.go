package provider

type Dash0CheckRule struct {
	Dataset       string                   `json:"dataset"`
	ID            string                   `json:"id,omitempty"`
	Name          string                   `json:"name"`
	Expression    string                   `json:"expression"`
	Thresholds    Dash0CheckRuleThresholds `json:"thresholds"`
	Summary       string                   `json:"summary"`
	Description   string                   `json:"description"`
	Interval      string                   `json:"interval,omitempty"`
	For           string                   `json:"for,omitempty"`
	KeepFiringFor string                   `json:"keepFiringFor,omitempty"`
	Labels        map[string]string        `json:"labels"`
	Annotations   map[string]string        `json:"annotations"`
	Enabled       bool                     `json:"enabled"`
}

type Dash0CheckRuleThresholds struct {
	Degraded int `json:"degraded"`
	Failed   int `json:"failed"`
}

type PrometheusRules struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   map[string]string   `yaml:"metadata"`
	Spec       PrometheusRulesSpec `yaml:"spec"`
}

type PrometheusRulesSpec struct {
	Groups []PrometheusRulesGroup `yaml:"groups"`
}

type PrometheusRulesGroup struct {
	Name     string           `yaml:"name"`
	Interval string           `yaml:"interval"`
	Rules    []PrometheusRule `yaml:"rules"`
}

type PrometheusRule struct {
	Alert         string            `yaml:"alert"`
	Expr          string            `yaml:"expr"`
	For           string            `yaml:"for"`
	KeepFiringFor string            `yaml:"keep_firing_for"`
	Annotations   map[string]string `yaml:"annotations"`
	Labels        map[string]string `yaml:"labels"`
}
