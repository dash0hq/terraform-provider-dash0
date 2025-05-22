package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckRuleOperations(t *testing.T) {
	// Test check rule data
	testOrigin := "test-check-rule"
	testDataset := "test-dataset"
	testYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adservice
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: adservice
          expr: (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != "", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != ""}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            description: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
          labels: {}`

	// Convert YAML to expected JSON for requests
	expectedJSON, err := ConvertYAMLToJSON(testYaml)
	require.NoError(t, err)

	checkRuleModel := checkRuleResourceModel{
		Origin:        types.StringValue(testOrigin),
		Dataset:       types.StringValue(testDataset),
		CheckRuleYaml: types.StringValue(testYaml),
	}

	tests := []struct {
		name           string
		operation      string
		expectedMethod string
		expectedPath   string
		expectedQuery  string
		expectedBody   string
		serverResponse string
		serverStatus   int
		expectError    bool
	}{
		{
			name:           "create check rule",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get check rule",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: testYaml,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "update check rule",
			operation:      "update",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"updated"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "delete check rule",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"status":"deleted"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get check rule - not found",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"error":"check rule not found"}`,
			serverStatus:   http.StatusNotFound,
			expectError:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				assert.Equal(t, tc.expectedMethod, r.Method)
				assert.Equal(t, tc.expectedPath, r.URL.Path)

				// Verify query parameters
				assert.Equal(t, tc.expectedQuery, r.URL.RawQuery)

				// Verify headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				// If there's a body to check (for PUT/POST)
				if tc.expectedBody != "" {
					bodyBytes, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Equal(t, tc.expectedBody, string(bodyBytes))
				}

				// Send response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.serverStatus)
				_, err := w.Write([]byte(tc.serverResponse))
				require.NoError(t, err)
			}))
			defer server.Close()

			// Create client
			client := newDash0Client(server.URL, "test-token")
			ctx := context.Background()
			var err error

			// Execute the operation based on the test case
			switch tc.operation {
			case "create":
				err = client.CreateCheckRule(ctx, checkRuleModel)
			case "get":
				var checkRule *checkRuleResourceModel
				checkRule, err = client.GetCheckRule(ctx, testDataset, testOrigin)
				if err == nil {
					assert.Equal(t, testOrigin, checkRule.Origin.ValueString())
					assert.Equal(t, testDataset, checkRule.Dataset.ValueString())
					assert.Equal(t, testYaml, checkRule.CheckRuleYaml.ValueString())
				}
			case "update":
				err = client.UpdateCheckRule(ctx, checkRuleModel)
			case "delete":
				err = client.DeleteCheckRule(ctx, testOrigin, testDataset)
			}

			// Assert results
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckRuleOperations_IntegrationStyle(t *testing.T) {
	// This test uses a more realistic HTTP server that records requests and returns
	// predefined responses based on the request path and method.

	// Setup test server that keeps track of requests
	var receivedRequests []*http.Request
	var receivedBodies []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record the request for later inspection
		receivedRequests = append(receivedRequests, r)

		// Read body if present
		if r.Body != nil {
			bodyBytes, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1048576))
			receivedBodies = append(receivedBodies, string(bodyBytes))
		} else {
			receivedBodies = append(receivedBodies, "")
		}

		// Default response
		status := http.StatusOK
		response := map[string]interface{}{
			"status": "success",
		}

		// Customize response based on the request
		if r.URL.Path == "/api/check-rules/test-check-rule" {
			if r.Method == http.MethodGet {
				// Return a check rule YAML for GET requests
				w.Header().Set("Content-Type", "application/yaml")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adservice
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: adservice
          expr: (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != "", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != ""}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            description: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
          labels: {}`))
				return
			} else if r.Method == http.MethodDelete {
				response["status"] = "deleted"
			} else if r.Method == http.MethodPut {
				response["status"] = "created_or_updated"
			}
		} else if r.URL.Path == "/api/check-rules/non-existent" {
			status = http.StatusNotFound
			response = map[string]interface{}{
				"status":  "error",
				"message": "not found",
			}
		}

		// Send JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := newDash0Client(server.URL, "test-token")

	// Test check rule data
	testOrigin := "test-check-rule"
	testDataset := "test-dataset"
	testYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adservice
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: adservice
          expr: (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != "", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != ""}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            description: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
          labels: {}`

	checkRuleModel := checkRuleResourceModel{
		Origin:        types.StringValue(testOrigin),
		Dataset:       types.StringValue(testDataset),
		CheckRuleYaml: types.StringValue(testYaml),
	}

	// Execute a complete CRUD workflow
	ctx := context.Background()

	// 1. Create check rule
	t.Run("create check rule", func(t *testing.T) {
		err := client.CreateCheckRule(ctx, checkRuleModel)
		require.NoError(t, err)

		// Check last request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
		
		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")
		
		// Verify JSON contains expected fields
		assert.Equal(t, "monitoring.coreos.com/v1", jsonObj["apiVersion"])
		assert.Equal(t, "PrometheusRule", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
	})

	// 2. Get check rule
	t.Run("get check rule", func(t *testing.T) {
		checkRule, err := client.GetCheckRule(ctx, testDataset, testOrigin)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodGet, lastReq.Method)
		assert.Equal(t, "/api/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Check response parsing
		assert.Equal(t, testOrigin, checkRule.Origin.ValueString())
		assert.Equal(t, testDataset, checkRule.Dataset.ValueString())
		assert.Equal(t, testYaml, checkRule.CheckRuleYaml.ValueString())
	})

	// 3. Update check rule
	t.Run("update check rule", func(t *testing.T) {
		// Update check rule YAML
		updatedYaml := testYaml + "\n# Updated check rule"
		updatedModel := checkRuleModel
		updatedModel.CheckRuleYaml = types.StringValue(updatedYaml)

		err := client.UpdateCheckRule(ctx, updatedModel)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
		
		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")
		
		// Verify JSON contains expected fields
		assert.Equal(t, "monitoring.coreos.com/v1", jsonObj["apiVersion"])
		assert.Equal(t, "PrometheusRule", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
	})

	// 4. Delete check rule
	t.Run("delete check rule", func(t *testing.T) {
		err := client.DeleteCheckRule(ctx, testOrigin, testDataset)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodDelete, lastReq.Method)
		assert.Equal(t, "/api/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
	})

	// 5. Test error handling with non-existent check rule
	t.Run("get non-existent check rule", func(t *testing.T) {
		_, err := client.GetCheckRule(ctx, testDataset, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API error (404)")
	})
}