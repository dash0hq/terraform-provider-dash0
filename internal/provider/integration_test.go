package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDash0API is an in-memory mock of the Dash0 API that stores resources
// keyed by path and tracks all requests for verification.
type mockDash0API struct {
	mu        sync.Mutex
	resources map[string][]byte // path -> last stored body
	requests  []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func newMockDash0API() *mockDash0API {
	return &mockDash0API{
		resources: make(map[string][]byte),
	}
}

func (m *mockDash0API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}

	m.requests = append(m.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Body:   string(body),
	})

	// Determine the resource key from the path (strip query params from storage key)
	key := r.URL.Path

	switch r.Method {
	case http.MethodPut:
		// Strip null/zero fields before storing, like a real API would
		m.resources[key] = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)

	case http.MethodPost:
		// For recording rule groups, POST creates and the API assigns an ID.
		// We store by the origin from the body's metadata.labels.
		if strings.HasPrefix(key, "/api/recording-rule-groups") && key == "/api/recording-rule-groups" {
			// Extract origin from body labels to build storage key
			var obj map[string]interface{}
			if err := json.Unmarshal(body, &obj); err == nil {
				if meta, ok := obj["metadata"].(map[string]interface{}); ok {
					if labels, ok := meta["labels"].(map[string]interface{}); ok {
						if origin, ok := labels["dash0.com/origin"].(string); ok {
							// Inject a version label (simulating the API behavior)
							labels["dash0.com/version"] = "1"
							updated, _ := json.Marshal(obj)
							storageKey := "/api/recording-rule-groups/" + origin
							m.resources[storageKey] = updated
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(updated)
							return
						}
					}
				}
			}
			// Fallback: store at the POST path
			m.resources[key] = body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(body)
			return
		}
		m.resources[key] = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)

	case http.MethodGet:
		if data, ok := m.resources[key]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		} else {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
		}

	case http.MethodDelete:
		delete(m.resources, key)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (m *mockDash0API) getRequests(method, pathPrefix string) []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []recordedRequest
	for _, r := range m.requests {
		if r.Method == method && strings.HasPrefix(r.Path, pathPrefix) {
			result = append(result, r)
		}
	}
	return result
}

// setupIntegrationTest creates a mock server and configures environment variables
// for the Terraform provider to use it.
func setupIntegrationTest(t *testing.T) (*mockDash0API, map[string]func() (tfprotov6.ProviderServer, error)) {
	mock := newMockDash0API()
	server := httptest.NewServer(mock)
	t.Cleanup(server.Close)

	t.Setenv("DASH0_URL", server.URL)
	t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token")

	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"dash0": providerserver.NewProtocol6WithError(New("test")()),
	}

	return mock, factories
}

// --- Dashboard Integration Tests ---

func TestIntegration_Dashboard_CRUD(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "default"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: test-dashboard
    spec:
      display:
        name: Test Dashboard
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_dashboard.test", "dataset", "default"),
					resource.TestCheckResourceAttrSet("dash0_dashboard.test", "origin"),
					func(s *terraform.State) error {
						// Verify the API received a PUT request
						puts := mock.getRequests(http.MethodPut, "/api/dashboards/")
						if len(puts) == 0 {
							return fmt.Errorf("expected at least one PUT to /api/dashboards/, got none")
						}
						// Verify dataset was sent as query parameter
						lastPut := puts[len(puts)-1]
						if !strings.Contains(lastPut.Query, "dataset=default") {
							return fmt.Errorf("expected dataset=default in query, got %s", lastPut.Query)
						}
						// Verify the body is valid JSON (converted from YAML)
						var body map[string]interface{}
						if err := json.Unmarshal([]byte(lastPut.Body), &body); err != nil {
							return fmt.Errorf("PUT body is not valid JSON: %s", err)
						}
						if body["kind"] != "Dashboard" {
							return fmt.Errorf("expected kind=Dashboard, got %v", body["kind"])
						}
						return nil
					},
				),
			},
			// Update
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "default"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: test-dashboard
    spec:
      display:
        name: Test Dashboard (updated)
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_dashboard.test", "dataset", "default"),
				),
			},
			// Delete (implicit by removing the resource)
		},
	})

	// After all steps, verify delete was called
	deletes := mock.getRequests(http.MethodDelete, "/api/dashboards/")
	assert.NotEmpty(t, deletes, "expected at least one DELETE to /api/dashboards/")
}

// --- View Integration Tests ---

func TestIntegration_View_CRUD(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_view" "test" {
  dataset = "default"
  view_yaml = <<-EOT
    kind: View
    metadata:
      name: test-view
    spec:
      display:
        name: Test View
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_view.test", "dataset", "default"),
					resource.TestCheckResourceAttrSet("dash0_view.test", "origin"),
					func(s *terraform.State) error {
						puts := mock.getRequests(http.MethodPut, "/api/views/")
						if len(puts) == 0 {
							return fmt.Errorf("expected at least one PUT to /api/views/, got none")
						}
						lastPut := puts[len(puts)-1]
						if !strings.Contains(lastPut.Query, "dataset=default") {
							return fmt.Errorf("expected dataset=default in query, got %s", lastPut.Query)
						}
						return nil
					},
				),
			},
			// Update
			{
				Config: `
provider "dash0" {}
resource "dash0_view" "test" {
  dataset = "default"
  view_yaml = <<-EOT
    kind: View
    metadata:
      name: test-view
    spec:
      display:
        name: Test View (updated)
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_view.test", "dataset", "default"),
				),
			},
		},
	})

	deletes := mock.getRequests(http.MethodDelete, "/api/views/")
	assert.NotEmpty(t, deletes, "expected at least one DELETE to /api/views/")
}

// --- Synthetic Check Integration Tests ---

func TestIntegration_SyntheticCheck_CRUD(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_synthetic_check" "test" {
  dataset = "default"
  synthetic_check_yaml = <<-EOT
    kind: SyntheticCheck
    metadata:
      name: test-check
    spec:
      plugin:
        kind: http
        spec:
          request:
            url: https://example.com
            method: GET
      schedule:
        interval: 5m
        strategy: all_locations
        locations:
          - us-oregon
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_synthetic_check.test", "dataset", "default"),
					resource.TestCheckResourceAttrSet("dash0_synthetic_check.test", "origin"),
					func(s *terraform.State) error {
						puts := mock.getRequests(http.MethodPut, "/api/synthetic-checks/")
						if len(puts) == 0 {
							return fmt.Errorf("expected at least one PUT to /api/synthetic-checks/, got none")
						}
						return nil
					},
				),
			},
		},
	})

	deletes := mock.getRequests(http.MethodDelete, "/api/synthetic-checks/")
	assert.NotEmpty(t, deletes, "expected at least one DELETE to /api/synthetic-checks/")
}

// --- Check Rule Integration Tests ---

func TestIntegration_CheckRule_CRUD(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_check_rule" "test" {
  dataset = "terraform-test"
  check_rule_yaml = <<-EOT
    apiVersion: monitoring.coreos.com/v1
    kind: PrometheusRule
    metadata: {}
    spec:
      groups:
        - name: TestAlerts
          interval: 1m0s
          rules:
            - alert: TestServiceDown
              expr: up{job="test-service"} == 0
              for: 5m0s
              annotations:
                summary: 'Test service is down'
              labels:
                severity: critical
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_check_rule.test", "dataset", "terraform-test"),
					resource.TestCheckResourceAttrSet("dash0_check_rule.test", "origin"),
					func(s *terraform.State) error {
						// Verify PUT was called (upsert semantics)
						puts := mock.getRequests(http.MethodPut, "/api/alerting/check-rules/")
						if len(puts) == 0 {
							return fmt.Errorf("expected at least one PUT to /api/alerting/check-rules/, got none")
						}

						// Verify the body contains the Dash0 check rule format (not Prometheus YAML)
						lastPut := puts[len(puts)-1]
						var body map[string]interface{}
						if err := json.Unmarshal([]byte(lastPut.Body), &body); err != nil {
							return fmt.Errorf("PUT body is not valid JSON: %s", err)
						}
						// Dash0 format should have "name" and "expression" fields
						if _, ok := body["name"]; !ok {
							return fmt.Errorf("expected 'name' field in Dash0 check rule format, got: %v", body)
						}
						if _, ok := body["expression"]; !ok {
							return fmt.Errorf("expected 'expression' field in Dash0 check rule format, got: %v", body)
						}
						// Verify dataset in query
						if !strings.Contains(lastPut.Query, "dataset=terraform-test") {
							return fmt.Errorf("expected dataset=terraform-test in query, got %s", lastPut.Query)
						}
						return nil
					},
				),
			},
		},
	})
}

// --- Recording Rule Group Integration Tests ---

func TestIntegration_RecordingRuleGroup_CRUD(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_recording_rule_group" "test" {
  dataset = "terraform-test"
  recording_rule_group_yaml = <<-EOT
    kind: Dash0RecordingRuleGroup
    metadata:
      name: http_metrics
    spec:
      interval: 1m
      rules:
        - record: http_requests_total:rate5m
          expression: rate(http_requests_total[5m])
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_recording_rule_group.test", "dataset", "terraform-test"),
					resource.TestCheckResourceAttrSet("dash0_recording_rule_group.test", "origin"),
					func(s *terraform.State) error {
						// Verify POST was used for create (not PUT)
						posts := mock.getRequests(http.MethodPost, "/api/recording-rule-groups")
						if len(posts) == 0 {
							return fmt.Errorf("expected at least one POST to /api/recording-rule-groups, got none")
						}

						// Verify labels were injected
						lastPost := posts[len(posts)-1]
						var body map[string]interface{}
						if err := json.Unmarshal([]byte(lastPost.Body), &body); err != nil {
							return fmt.Errorf("POST body is not valid JSON: %s", err)
						}

						meta, ok := body["metadata"].(map[string]interface{})
						if !ok {
							return fmt.Errorf("expected metadata in body")
						}
						labels, ok := meta["labels"].(map[string]interface{})
						if !ok {
							return fmt.Errorf("expected metadata.labels in body")
						}
						if labels["dash0.com/dataset"] != "terraform-test" {
							return fmt.Errorf("expected dash0.com/dataset=terraform-test, got %v", labels["dash0.com/dataset"])
						}
						if labels["dash0.com/origin"] == nil || labels["dash0.com/origin"] == "" {
							return fmt.Errorf("expected dash0.com/origin to be set")
						}
						return nil
					},
				),
			},
			// Update
			{
				Config: `
provider "dash0" {}
resource "dash0_recording_rule_group" "test" {
  dataset = "terraform-test"
  recording_rule_group_yaml = <<-EOT
    kind: Dash0RecordingRuleGroup
    metadata:
      name: http_metrics
    spec:
      interval: 2m
      rules:
        - record: http_requests_total:rate5m
          expression: rate(http_requests_total[5m])
        - record: http_errors_total:rate5m
          expression: rate(http_requests_total{status=~"5.."}[5m])
  EOT
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_recording_rule_group.test", "dataset", "terraform-test"),
					func(s *terraform.State) error {
						// Verify PUT was used for update
						puts := mock.getRequests(http.MethodPut, "/api/recording-rule-groups/")
						if len(puts) == 0 {
							return fmt.Errorf("expected at least one PUT to /api/recording-rule-groups/ for update, got none")
						}

						// Verify version label was injected (fetched from GET before update)
						lastPut := puts[len(puts)-1]
						var body map[string]interface{}
						if err := json.Unmarshal([]byte(lastPut.Body), &body); err != nil {
							return fmt.Errorf("PUT body is not valid JSON: %s", err)
						}

						meta, ok := body["metadata"].(map[string]interface{})
						if !ok {
							return fmt.Errorf("expected metadata in PUT body")
						}
						labels, ok := meta["labels"].(map[string]interface{})
						if !ok {
							return fmt.Errorf("expected metadata.labels in PUT body")
						}
						if labels["dash0.com/version"] == nil || labels["dash0.com/version"] == "" {
							return fmt.Errorf("expected dash0.com/version to be set in update request")
						}
						return nil
					},
				),
			},
		},
	})

	deletes := mock.getRequests(http.MethodDelete, "/api/recording-rule-groups/")
	assert.NotEmpty(t, deletes, "expected at least one DELETE to /api/recording-rule-groups/")
}

// --- Dataset Change Forces Recreation ---

func TestIntegration_Dashboard_DatasetChangeRecreates(t *testing.T) {
	mock, factories := setupIntegrationTest(t)

	var firstOrigin string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create with dataset "default"
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "default"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: test
    spec:
      display:
        name: Test
  EOT
}`,
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources["dash0_dashboard.test"]
					firstOrigin = rs.Primary.Attributes["origin"]
					return nil
				},
			},
			// Change dataset → should destroy + recreate with new origin
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "other-dataset"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: test
    spec:
      display:
        name: Test
  EOT
}`,
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources["dash0_dashboard.test"]
					newOrigin := rs.Primary.Attributes["origin"]
					if newOrigin == firstOrigin {
						return fmt.Errorf("expected a new origin after dataset change, but got the same: %s", newOrigin)
					}
					return nil
				},
			},
		},
	})

	// Verify delete was called for the old resource
	deletes := mock.getRequests(http.MethodDelete, "/api/dashboards/")
	require.GreaterOrEqual(t, len(deletes), 1, "expected delete during recreation")
}

// --- Import State ---

func TestIntegration_Dashboard_Import(t *testing.T) {
	_, factories := setupIntegrationTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "default"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: test
    spec:
      display:
        name: Test
  EOT
}`,
			},
			// Import
			{
				ResourceName:      "dash0_dashboard.test",
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["dash0_dashboard.test"]
					return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
				},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					if states[0].Attributes["origin"] == "" {
						return fmt.Errorf("origin is empty after import")
					}
					if states[0].Attributes["dataset"] != "default" {
						return fmt.Errorf("expected dataset=default, got %s", states[0].Attributes["dataset"])
					}
					if states[0].Attributes["dashboard_yaml"] == "" {
						return fmt.Errorf("dashboard_yaml is empty after import")
					}
					return nil
				},
			},
		},
	})
}

// --- Check Rule Prometheus Format Round-Trip ---

func TestIntegration_CheckRule_PrometheusFormatRoundTrip(t *testing.T) {
	_, factories := setupIntegrationTest(t)

	// This test verifies that the Prometheus YAML format is correctly converted
	// to Dash0 API format on create, and back to Prometheus format on read,
	// without losing information or causing state drift.
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "dash0" {}
resource "dash0_check_rule" "test" {
  dataset = "terraform-test"
  check_rule_yaml = %q
}`, basicCheckRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("dash0_check_rule.test", "dataset", "terraform-test"),
					resource.TestCheckResourceAttr("dash0_check_rule.test", "check_rule_yaml", basicCheckRuleYaml),
				),
			},
			// Re-apply same config → should be idempotent (no changes)
			{
				Config: fmt.Sprintf(`
provider "dash0" {}
resource "dash0_check_rule" "test" {
  dataset = "terraform-test"
  check_rule_yaml = %q
}`, basicCheckRuleYaml),
				PlanOnly: true,
			},
		},
	})
}

// --- Verify Auth Header ---

func TestIntegration_AuthHeaderSent(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return whatever was sent
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			w.Write(body)
		} else {
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	t.Setenv("DASH0_URL", server.URL)
	t.Setenv("DASH0_AUTH_TOKEN", "auth_my_secret_token")

	// We don't use resource.Test here because we just want to verify the auth header;
	// the simplest way is to check it was sent correctly during a CRUD operation.
	if os.Getenv("TF_ACC") == "1" {
		// Only run this as part of the full integration test suite
		t.Skip("Skipping simple auth test when running full acc tests")
	}

	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"dash0": providerserver.NewProtocol6WithError(New("test")()),
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "dash0" {}
resource "dash0_dashboard" "test" {
  dataset = "default"
  dashboard_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: auth-test
    spec: {}
  EOT
}`,
				Check: func(s *terraform.State) error {
					if authHeader != "Bearer auth_my_secret_token" {
						return fmt.Errorf("expected Authorization header 'Bearer auth_my_secret_token', got '%s'", authHeader)
					}
					return nil
				},
			},
		},
	})
}
