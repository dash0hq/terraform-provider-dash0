package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type transformationRuleTestAPI struct {
	mu     sync.Mutex
	rules  map[string]map[string]interface{}
	nextID int
}

func (a *transformationRuleTestAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/api/datasets/") {
		_, _ = w.Write([]byte(`{}`))
		return
	}
	const collection = "/api/telemetry-transformation-rules"
	identifier := strings.TrimPrefix(r.URL.Path, collection+"/")
	dataset := r.URL.Query().Get("dataset")
	var key string
	for candidate, rule := range a.rules {
		labels := rule["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		if labels["dash0.com/dataset"] == dataset && (labels["dash0.com/origin"] == identifier || labels["dash0.com/id"] == identifier) {
			key = candidate
			break
		}
	}
	if r.Method != http.MethodPost && key == "" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"rule not found"}}`))
		return
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		if r.Method == http.MethodPost && r.URL.Path != collection {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var rule map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		metadata := rule["metadata"].(map[string]interface{})
		labels := metadata["labels"].(map[string]interface{})
		if r.Method == http.MethodPost {
			a.nextID++
			labels["dash0.com/id"] = fmt.Sprintf("ttr_%d", a.nextID)
			key = dataset + "/" + labels["dash0.com/origin"].(string)
		} else {
			stored := a.rules[key]["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
			if origin := labels["dash0.com/origin"]; origin != nil && origin != stored["dash0.com/origin"] {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			labels["dash0.com/id"] = stored["dash0.com/id"]
			labels["dash0.com/origin"] = stored["dash0.com/origin"]
		}
		labels["dash0.com/source"] = "terraform"
		metadata["annotations"] = map[string]interface{}{"dash0.com/updated-at": "2026-11-04T11:00:00.123456789Z"}
		spec := rule["spec"].(map[string]interface{})
		if spec["priority"] == nil {
			spec["priority"] = float64(200)
		}
		a.rules[key] = rule
		_ = json.NewEncoder(w).Encode(rule)
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(a.rules[key])
	case http.MethodDelete:
		delete(a.rules, key)
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func setupTransformationRuleIntegration(t *testing.T) (*transformationRuleTestAPI, map[string]func() (tfprotov6.ProviderServer, error)) {
	t.Helper()
	a := &transformationRuleTestAPI{rules: make(map[string]map[string]interface{})}
	server := httptest.NewServer(a)
	t.Cleanup(server.Close)
	t.Setenv("DASH0_URL", server.URL)
	t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token")
	t.Setenv("DASH0_DATASET", "")
	return a, map[string]func() (tfprotov6.ProviderServer, error){
		"dash0": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func transformationRuleIntegrationConfig(document, datasetAttribute string) string {
	return fmt.Sprintf(`
provider "dash0" {
  dataset = "production"
}
resource "dash0_telemetry_transformation_rule" "test" {
  %s
  telemetry_transformation_rule_yaml = %q
}`, datasetAttribute, document)
}

func (a *transformationRuleTestAPI) checkRule(document string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs := s.RootModule().Resources["dash0_telemetry_transformation_rule.test"].Primary.Attributes
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, actual := range a.rules {
			labels := actual["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
			if labels["dash0.com/id"] != attrs["id"] {
				continue
			}
			if labels["dash0.com/dataset"] != attrs["dataset"] {
				return fmt.Errorf("dataset differs: API %v, state %s", labels["dash0.com/dataset"], attrs["dataset"])
			}
			var expected map[string]interface{}
			if err := yaml.Unmarshal([]byte(document), &expected); err != nil {
				return err
			}
			expectedSpec := expected["spec"].(map[string]interface{})
			if expectedSpec["priority"] == nil {
				expectedSpec["priority"] = 200
			}
			want, _ := json.Marshal(expectedSpec)
			got, _ := json.Marshal(actual["spec"])
			if string(want) != string(got) {
				return fmt.Errorf("stored spec differs:\nwant %s\ngot  %s", want, got)
			}
			return nil
		}
		return fmt.Errorf("state ID %s was not stored by the API", attrs["id"])
	}
}

func TestIntegration_TelemetryTransformationRule_Lifecycle(t *testing.T) {
	a, factories := setupTransformationRuleIntegration(t)
	reordered := strings.Replace(telemetryTransformationRuleYAML,
		"        - 'set(attributes[\"stage\"], \"one\")'\n        - 'set(attributes[\"stage\"], \"two\")'",
		"        - 'set(attributes[\"stage\"], \"two\")'\n        - 'set(attributes[\"stage\"], \"one\")'", 1)
	require.NotEqual(t, telemetryTransformationRuleYAML, reordered)
	changedExample := strings.Replace(reordered, "sample log", "updated sample log", 1)
	withoutExamples, _, _ := strings.Cut(changedExample, "  examples:")
	var previousOrigin, previousID string
	checkNewIdentity := func(s *terraform.State) error {
		attrs := s.RootModule().Resources["dash0_telemetry_transformation_rule.test"].Primary.Attributes
		if !strings.HasPrefix(attrs["origin"], "tf_") || attrs["origin"] == previousOrigin || attrs["id"] == previousID {
			return fmt.Errorf("expected fresh origin and ID, got %v", attrs)
		}
		previousOrigin, previousID = attrs["origin"], attrs["id"]
		return nil
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: transformationRuleIntegrationConfig(telemetryTransformationRuleYAML, ""),
				Check: resource.ComposeAggregateTestCheckFunc(a.checkRule(telemetryTransformationRuleYAML), checkNewIdentity,
					resource.TestCheckResourceAttr("dash0_telemetry_transformation_rule.test", "dataset", "production")),
			},
			{
				Config:   transformationRuleIntegrationConfig(strings.Replace(telemetryTransformationRuleYAML, "spec:\n", "spec:\n  priority: 200\n", 1), ""),
				PlanOnly: true,
			},
			{
				Config: transformationRuleIntegrationConfig(reordered, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("dash0_telemetry_transformation_rule.test", plancheck.ResourceActionUpdate),
				}},
				Check: a.checkRule(reordered),
			},
			{Config: transformationRuleIntegrationConfig(changedExample, ""), Check: a.checkRule(changedExample)},
			{Config: transformationRuleIntegrationConfig(withoutExamples, ""), Check: a.checkRule(withoutExamples)},
			{
				Config: transformationRuleIntegrationConfig(withoutExamples, `dataset = "other"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("dash0_telemetry_transformation_rule.test", plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(a.checkRule(withoutExamples), checkNewIdentity),
			},
			{
				PreConfig: func() {
					a.mu.Lock()
					defer a.mu.Unlock()
					a.rules = make(map[string]map[string]interface{})
				},
				Config: transformationRuleIntegrationConfig(withoutExamples, `dataset = "other"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("dash0_telemetry_transformation_rule.test", plancheck.ResourceActionCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(a.checkRule(withoutExamples), checkNewIdentity),
			},
		},
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	require.Empty(t, a.rules, "destroy must delete the rule, including the one replaced in the old dataset")
}

func TestIntegration_TelemetryTransformationRule_Import(t *testing.T) {
	for _, identifier := range []string{"tf_origin", "ttr_rule-id"} {
		t.Run(identifier, func(t *testing.T) {
			a, factories := setupTransformationRuleIntegration(t)
			var rule map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(telemetryTransformationRuleAPIJSON), &rule))
			a.rules["production/tf_origin"] = rule
			config := transformationRuleIntegrationConfig(telemetryTransformationRuleYAML, `dataset = "production"`)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: config, ResourceName: "dash0_telemetry_transformation_rule.test",
						ImportState: true, ImportStateId: "production," + identifier, ImportStatePersist: true,
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 || states[0].Attributes["origin"] != identifier || !strings.Contains(states[0].Attributes[telemetryTransformationRuleYAMLAttribute], "sample log") {
								return fmt.Errorf("import lost the supplied identifier or full examples: %v", states)
							}
							return nil
						},
					},
					{Config: config, PlanOnly: true},
					{
						Config: transformationRuleIntegrationConfig(strings.Replace(telemetryTransformationRuleYAML, "sample log", "updated sample log", 1), `dataset = "production"`),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("dash0_telemetry_transformation_rule.test", "origin", identifier),
							resource.TestCheckResourceAttr("dash0_telemetry_transformation_rule.test", "id", "ttr_rule-id"),
							a.checkRule(strings.Replace(telemetryTransformationRuleYAML, "sample log", "updated sample log", 1))),
					},
				},
			})
			a.mu.Lock()
			defer a.mu.Unlock()
			require.Empty(t, a.rules)
		})
	}
}
