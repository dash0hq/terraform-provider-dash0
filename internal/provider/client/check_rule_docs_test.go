package client

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dash0yaml "github.com/dash0hq/dash0-api-client-go/yaml"
)

var (
	checkRuleHeredoc  = regexp.MustCompile(`(?s)check_rule_yaml = <<-EOF\n(.*?)\nEOF`)
	terraformInterpol = regexp.MustCompile(`\$\{[^}]*\}`)
	metadataName      = regexp.MustCompile(`(?m)^\s*name: (.+)$`)
)

// TestCheckRuleDocExamplesParse runs every check_rule_yaml document in the
// generated resource page through the same parse the provider performs on
// create and update.
//
// UnmarshalPrometheusRule accepts exactly one group containing one rule, so a
// documented example carrying more than that is not merely stylistically off:
// it fails at apply time for anyone who copies it. The docs are generated from
// templates/resources/check_rule.md.tmpl, where nothing else checks the YAML.
func TestCheckRuleDocExamplesParse(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/resources/check_rule.md")
	require.NoError(t, err, "read generated resource docs")

	matches := checkRuleHeredoc.FindAllStringSubmatch(string(raw), -1)
	require.NotEmpty(t, matches, "expected at least one check_rule_yaml example in the docs")

	for _, match := range matches {
		// Terraform interpolations are not YAML, so stand them in for a scalar.
		doc := terraformInterpol.ReplaceAllString(match[1], "placeholder")

		name := "<unnamed>"
		if n := metadataName.FindStringSubmatch(doc); n != nil {
			name = strings.TrimSpace(n[1])
		}

		t.Run(name, func(t *testing.T) {
			_, err := dash0yaml.UnmarshalPrometheusRule([]byte(doc))
			require.NoError(t, err, "documented example must parse")
		})
	}
}
