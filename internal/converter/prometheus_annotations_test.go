package converter

import (
	"strings"
	"testing"
)

func TestMoveTopLevelAnnotationsIntoRules(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// contains are substrings the result must have.
		contains []string
		// absent are substrings the result must not have.
		absent []string
		// unchanged asserts the input is returned verbatim.
		unchanged bool
	}{
		{
			name: "rule-level annotation wins over top-level",
			in: `metadata:
  annotations:
    shared: top
    only-top: yes
spec:
  groups:
    - name: g
      rules:
        - alert: A
          annotations:
            shared: rule
`,
			contains: []string{"shared: rule", "only-top:"},
			absent:   []string{"shared: top"},
		},
		{
			name: "top-level map is removed once moved",
			in: `metadata:
  name: keep-me
  annotations:
    a: b
spec:
  groups:
    - name: g
      rules:
        - alert: A
`,
			contains: []string{"a: b", "name: keep-me"},
			absent:   []string{"annotations:\n        a: b"},
		},
		{
			// The critical and degraded thresholds are separate annotations, so
			// one may come from the top level and the other from the rule. They
			// do not collide on a key, and the merge is per key rather than a
			// map replacement, so both must land on the rule.
			name: "thresholds set at different levels both land on the rule",
			in: `metadata:
  annotations:
    dash0-threshold-critical: "1000"
spec:
  groups:
    - name: g
      rules:
        - alert: A
          annotations:
            dash0-threshold-degraded: "500"
`,
			contains: []string{`dash0-threshold-critical: "1000"`, `dash0-threshold-degraded: "500"`},
		},
		{
			// Annotation values are strings on the wire, and the merge goes
			// through dash0yaml.MergeAnnotations, which is typed on
			// map[string]string. An unquoted YAML scalar therefore renders to
			// its string form, matching what the API stores.
			name: "non-string annotation values are rendered as strings",
			in: `metadata:
  annotations:
    dash0-threshold-critical: 5000
    dash0-enabled: true
spec:
  groups:
    - name: g
      rules:
        - alert: A
`,
			contains: []string{`dash0-threshold-critical: "5000"`, `dash0-enabled: "true"`},
		},
		{
			// A key with no value unmarshals to nil. Rendering it with %v would
			// produce the literal "<nil>", which no API response can match, so
			// the resource would drift on every plan.
			name: "empty annotation value becomes an empty string, not <nil>",
			in: `metadata:
  annotations:
    k:
spec:
  groups:
    - name: g
      rules:
        - alert: A
`,
			contains: []string{`k: ""`},
			absent:   []string{"<nil>"},
		},
		{
			name:      "unparseable YAML is returned unchanged",
			in:        "{{ not yaml",
			unchanged: true,
		},
		{
			name: "group without rules is returned unchanged",
			in: `metadata:
  annotations:
    a: b
spec:
  groups:
    - name: g
`,
			unchanged: true,
		},
		{
			name: "annotations that are not a map are returned unchanged",
			in: `metadata:
  annotations: "just a string"
spec:
  groups:
    - name: g
      rules:
        - alert: A
`,
			unchanged: true,
		},
		{
			name: "rule annotations that are not a map are overwritten by the move",
			in: `metadata:
  annotations:
    a: b
spec:
  groups:
    - name: g
      rules:
        - alert: A
          annotations:
            - not
            - a map
`,
			contains: []string{"a: b"},
			absent:   []string{"not a map"},
		},
		{
			name: "no top-level annotations is a no-op",
			in: `metadata:
  name: t
spec:
  groups:
    - name: g
      rules:
        - alert: A
`,
			unchanged: true,
		},
		{
			name:      "document without spec is returned unchanged",
			in:        "metadata:\n  annotations:\n    a: b\n",
			unchanged: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MoveTopLevelAnnotationsIntoRules(tc.in)

			if tc.unchanged {
				if got != tc.in {
					t.Fatalf("expected input returned unchanged, got:\n%s", got)
				}
				return
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected result to contain %q, got:\n%s", want, got)
				}
			}
			for _, notWant := range tc.absent {
				if strings.Contains(got, notWant) {
					t.Errorf("expected result not to contain %q, got:\n%s", notWant, got)
				}
			}
		})
	}
}
