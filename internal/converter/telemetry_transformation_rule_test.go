package converter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const telemetryTransformationRuleYAML = `kind: Dash0TelemetryTransformationRule
metadata:
  name: normalize-logs
spec:
  enabled: true
  signal: logs
  context: log
  transformations:
    - name: redact
      statements:
        - 'set(attributes["z"], "secret")'
        - 'delete_key(attributes, "z")'
    - name: classify
      statements:
        - 'set(attributes["a"], "public")'
`

func TestTelemetryTransformationRuleYAMLEquivalent_Formatting(t *testing.T) {
	jsonDocument := `{"spec":{"transformations":[{"statements":["set(attributes[\"z\"], \"secret\")","delete_key(attributes, \"z\")"],"name":"redact"},{"name":"classify","statements":["set(attributes[\"a\"], \"public\")"]}],"context":"log","signal":"logs","enabled":true},"metadata":{"name":"normalize-logs"},"kind":"Dash0TelemetryTransformationRule"}`

	assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleYAML, jsonDocument, nil, true)
}

func TestTelemetryTransformationRuleYAMLEquivalent_Metadata(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		ignored []string
		want    bool
	}{
		{
			name: "server labels and timestamp annotations",
			from: "  name: normalize-logs\n",
			to: `  name: normalize-logs
  labels:
    dash0.com/id: 1234
    dash0.com/origin: tf_origin
    dash0.com/dataset: default
    dash0.com/source: terraform
  annotations:
    dash0.com/created-at: 2026-11-01T12:00:00Z
    dash0.com/updated-at: "2026-11-02T12:00:00Z"
`,
			want: true,
		},
		{name: "metadata name", from: "normalize-logs", to: "other", want: false},
		{name: "kind", from: "Dash0TelemetryTransformationRule", to: "OtherRule", want: false},
		{name: "api version retained", from: "kind:", to: "apiVersion: example/v1\nkind:", want: false},
		{name: "metadata timestamp outside annotations retained", from: "metadata:\n", to: "metadata:\n  createdAt: 2026-11-01T12:00:00Z\n", want: false},
		{name: "explicitly ignored metadata timestamp", from: "metadata:\n", to: "metadata:\n  createdAt: 2026-11-01T12:00:00Z\n", ignored: []string{"metadata.createdAt"}, want: true},
		{name: "explicitly ignored added field", from: "  context: log\n", to: "  context: log\n  permissions: [read]\n", ignored: []string{"spec.permissions"}, want: true},
		{name: "nonignored sibling retained", from: "  context: log\n", to: "  context: resource\n  permissions: [read]\n", ignored: []string{"spec.permissions"}, want: false},
		{name: "top-level labels retained", from: "kind:", to: "labels: {team: example}\nkind:", want: false},
		{name: "explicitly ignored nested field", from: "  context: log\n", to: "  context: log\n  display: {name: generated}\n", ignored: []string{"spec.display.name"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, 1, strings.Count(telemetryTransformationRuleYAML, tt.from))
			yamlB := strings.Replace(telemetryTransformationRuleYAML, tt.from, tt.to, 1)
			assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleYAML, yamlB, tt.ignored, tt.want)
		})
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_Priority(t *testing.T) {
	variants := []struct {
		name     string
		field    string
		semantic string
	}{
		{name: "omitted", semantic: "default"},
		{name: "null", field: "  priority: null\n", semantic: "default"},
		{name: "default", field: "  priority: 200\n", semantic: "default"},
		{name: "default as decimal", field: "  priority: 200.0\n", semantic: "default"},
		{name: "zero", field: "  priority: 0\n", semantic: "zero"},
		{name: "100", field: "  priority: 100\n", semantic: "100"},
		{name: "300", field: "  priority: 300\n", semantic: "300"},
		{name: "400", field: "  priority: 400\n", semantic: "400"},
		{name: "below default", field: "  priority: 199\n", semantic: "199"},
		{name: "above default", field: "  priority: 201\n", semantic: "201"},
		{name: "negative", field: "  priority: -1\n", semantic: "negative"},
		{name: "string", field: "  priority: '200'\n", semantic: "string"},
	}
	for _, a := range variants {
		for _, b := range variants {
			t.Run(a.name+"/"+b.name, func(t *testing.T) {
				yamlA := strings.Replace(telemetryTransformationRuleYAML, "spec:\n", "spec:\n"+a.field, 1)
				yamlB := strings.Replace(telemetryTransformationRuleYAML, "spec:\n", "spec:\n"+b.field, 1)
				assertTelemetryTransformationRuleEquivalent(t, yamlA, yamlB, nil, a.semantic == b.semantic)
			})
		}
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_OptionalLists(t *testing.T) {
	for _, field := range []string{"conditions", "examples"} {
		for _, valueA := range []string{"", "null", "[]"} {
			for _, valueB := range []string{"", "null", "[]"} {
				t.Run(field+"/"+valueA+"/"+valueB, func(t *testing.T) {
					yamlA, yamlB := telemetryTransformationRuleYAML, telemetryTransformationRuleYAML
					if valueA != "" {
						yamlA += "  " + field + ": " + valueA + "\n"
					}
					if valueB != "" {
						yamlB += "  " + field + ": " + valueB + "\n"
					}
					assertTelemetryTransformationRuleEquivalent(t, yamlA, yamlB, nil, true)
				})
			}
		}
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_OrderedContent(t *testing.T) {
	first := `    - name: redact
      statements:
        - 'set(attributes["z"], "secret")'
        - 'delete_key(attributes, "z")'
`
	second := `    - name: classify
      statements:
        - 'set(attributes["a"], "public")'
`
	reversedStatements := `    - name: redact
      statements:
        - 'delete_key(attributes, "z")'
        - 'set(attributes["z"], "secret")'
`
	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "transformations", from: first + second, to: second + first},
		{name: "statements in only the first transformation", from: first, to: reversedStatements},
		{name: "transformations and statements", from: first + second, to: second + reversedStatements},
		{name: "statement text", from: `"secret"`, to: `"redacted"`},
		{name: "transformation name", from: "name: redact", to: "name: scrub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, 1, strings.Count(telemetryTransformationRuleYAML, tt.from))
			yamlB := strings.Replace(telemetryTransformationRuleYAML, tt.from, tt.to, 1)
			assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleYAML, yamlB, nil, false)
		})
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_ExamplePayloads(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "empty string versus absent", a: `{"stringValue":""}`, b: `{}`, want: false},
		{name: "false versus absent", a: `{"boolValue":false}`, b: `{}`, want: false},
		{name: "zero versus absent", a: `{"doubleValue":0}`, b: `{}`, want: false},
		{name: "empty string versus null", a: `{"stringValue":""}`, b: `{"stringValue":null}`, want: false},
		{name: "false versus null", a: `{"boolValue":false}`, b: `{"boolValue":null}`, want: false},
		{name: "zero versus null", a: `{"doubleValue":0}`, b: `{"doubleValue":null}`, want: false},
		{name: "null versus absent", a: `{"stringValue":null}`, b: `{}`, want: false},
		{name: "number versus boolean", a: `{"doubleValue":0}`, b: `{"doubleValue":false}`, want: false},
		{name: "number versus string", a: `{"doubleValue":0}`, b: `{"doubleValue":"0"}`, want: false},
		{name: "empty map versus absent", a: `{"kvlistValue":{}}`, b: `{}`, want: false},
		{name: "empty array versus absent", a: `{"arrayValue":{"values":[]}}`, b: `{"arrayValue":{}}`, want: false},
		{name: "nested zero versus absent", a: `{"kvlistValue":{"values":[{"key":"zero","value":{"doubleValue":0}}]}}`, b: `{"kvlistValue":{"values":[{"key":"zero","value":{}}]}}`, want: false},
		{name: "nested array order", a: `{"arrayValue":{"values":[{"stringValue":"z"},{"stringValue":"a"}]}}`, b: `{"arrayValue":{"values":[{"stringValue":"a"},{"stringValue":"z"}]}}`, want: false},
		{name: "nested labels preserved", a: `{"metadata":{"labels":{"team":"one"}}}`, b: `{"metadata":{"labels":{"team":"two"}}}`, want: false},
		{name: "nested label types preserved", a: `{"labels":{"value":0}}`, b: `{"labels":{"value":"0"}}`, want: false},
		{name: "nested annotations preserved", a: `{"metadata":{"annotations":{"dash0.com/updated-at":"2026-11-01T12:00:00Z"}}}`, b: `{"metadata":{"annotations":{"dash0.com/updated-at":"2026-11-02T12:00:00Z"}}}`, want: false},
		{name: "nested priority preserved", a: `{"priority":200}`, b: `{}`, want: false},
		{name: "nested optional lists preserved", a: `{"conditions":[],"examples":[]}`, b: `{}`, want: false},
		{name: "duration strings preserved", a: `{"stringValue":"2m"}`, b: `{"stringValue":"120s"}`, want: false},
		{name: "zero duration preserved", a: `{"for":"0s"}`, b: `{}`, want: false},
		{name: "large integer precision", a: `{"doubleValue":9007199254740992}`, b: `{"doubleValue":9007199254740993}`, want: false},
		{name: "unsigned integer precision", a: `{"doubleValue":18446744073709551614}`, b: `{"doubleValue":18446744073709551615}`, want: false},
		{name: "number formatting", a: `{"doubleValue":1}`, b: `{"doubleValue":1.0}`, want: true},
		{name: "object key order", a: `{"kvlistValue":{"values":[{"key":"name","value":{"stringValue":"value"}}]}}`, b: `{"kvlistValue":{"values":[{"value":{"stringValue":"value"},"key":"name"}]}}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleWithExampleBody(tt.a), telemetryTransformationRuleWithExampleBody(tt.b), nil, tt.want)
		})
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_ExampleChanges(t *testing.T) {
	withExample := telemetryTransformationRuleWithExampleBody(`{"stringValue":"before"}`)
	t.Run("example added or removed", func(t *testing.T) {
		assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleYAML, withExample, nil, false)
	})
	t.Run("example renamed", func(t *testing.T) {
		renamed := strings.Replace(withExample, "name: example", "name: renamed", 1)
		assertTelemetryTransformationRuleEquivalent(t, withExample, renamed, nil, false)
	})
	t.Run("only example data changed", func(t *testing.T) {
		changed := telemetryTransformationRuleWithExampleBody(`{"stringValue":"after"}`)
		assertTelemetryTransformationRuleEquivalent(t, withExample, changed, nil, false)
	})
	t.Run("example order", func(t *testing.T) {
		first := `    - name: z-example
      otlp: {"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"first"}}]}]}]}
`
		second := `    - name: a-example
      otlp: {"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"second"}}]}]}]}
`
		yamlA := telemetryTransformationRuleYAML + "  examples:\n" + first + second
		yamlB := telemetryTransformationRuleYAML + "  examples:\n" + second + first
		assertTelemetryTransformationRuleEquivalent(t, yamlA, yamlB, nil, false)
	})
}

func TestTelemetryTransformationRuleYAMLEquivalent_SpecChanges(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "enabled changed", from: "enabled: true", to: "enabled: false"},
		{name: "signal changed", from: "signal: logs", to: "signal: traces"},
		{name: "context changed", from: "context: log", to: "context: resource"},
		{name: "display added", from: "spec:\n", to: "spec:\n  display: {name: Logs}\n"},
		{name: "conditions added", from: "spec:\n", to: "spec:\n  conditions: ['severity_number > 9']\n"},
		{name: "empty map is not an optional list", from: "spec:\n", to: "spec:\n  conditions: {}\n"},
		{name: "empty string is not an optional list", from: "spec:\n", to: "spec:\n  examples: ''\n"},
		{name: "unknown zero value is not stripped", from: "spec:\n", to: "spec:\n  custom: 0\n"},
		{name: "unknown false value is not stripped", from: "spec:\n", to: "spec:\n  custom: false\n"},
		{name: "unknown null value is not stripped", from: "spec:\n", to: "spec:\n  custom: null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlB := strings.Replace(telemetryTransformationRuleYAML, tt.from, tt.to, 1)
			assertTelemetryTransformationRuleEquivalent(t, telemetryTransformationRuleYAML, yamlB, nil, false)
		})
	}
}

func TestTelemetryTransformationRuleYAMLEquivalent_InvalidDocuments(t *testing.T) {
	for _, invalid := range []string{"invalid: : yaml", "", "null", "scalar", "[one, two]", telemetryTransformationRuleWithExampleBody(`{"doubleValue":.nan}`)} {
		t.Run(invalid, func(t *testing.T) {
			equivalent, err := TelemetryTransformationRuleYAMLEquivalent(invalid, telemetryTransformationRuleYAML, nil)
			require.ErrorContains(t, err, "first telemetry transformation rule YAML")
			assert.False(t, equivalent)
			equivalent, err = TelemetryTransformationRuleYAMLEquivalent(telemetryTransformationRuleYAML, invalid, nil)
			require.ErrorContains(t, err, "second telemetry transformation rule YAML")
			assert.False(t, equivalent)
		})
	}
}

func telemetryTransformationRuleWithExampleBody(body string) string {
	return telemetryTransformationRuleYAML + `  examples:
    - name: example
      otlp: {"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":` + body + `}]}]}]}
`
}

func assertTelemetryTransformationRuleEquivalent(t *testing.T, yamlA, yamlB string, ignored []string, want bool) {
	t.Helper()
	originalA, originalB := yamlA, yamlB

	equivalent, err := TelemetryTransformationRuleYAMLEquivalent(yamlA, yamlB, ignored)
	require.NoError(t, err)
	assert.Equal(t, want, equivalent, "forward comparison")
	equivalent, err = TelemetryTransformationRuleYAMLEquivalent(yamlB, yamlA, ignored)
	require.NoError(t, err)
	assert.Equal(t, want, equivalent, "reverse comparison")
	assert.Equal(t, originalA, yamlA)
	assert.Equal(t, originalB, yamlB)
}
