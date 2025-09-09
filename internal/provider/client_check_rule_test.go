package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
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
metadata: {}
spec:
  groups:
    - name: example-check-rules
      interval: 1m0s
      rules:
        - alert: HighMemoryUsage
          expr: memory_usage > 0.8
          for: 5m
          annotations:
            summary: High memory usage detected
          labels:
            severity: warning`

	// Convert YAML to expected JSON for requests (this will use the converter)
	dash0CheckRule, err := converter.ConvertPromYAMLToDash0CheckRule(testYaml, testDataset)
	require.NoError(t, err)
	expectedJSON, err := json.Marshal(dash0CheckRule)
	require.NoError(t, err)

	checkRuleModel := model.CheckRuleResourceModel{
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
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   string(expectedJSON),
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get check rule",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"dataset":"default","name":"example-check-rules - HighMemoryUsage","expression":"memory_usage > 0.8","thresholds":{"degraded":0,"failed":0},"summary":"High memory usage detected","description":"","interval":"1m0s","for":"5m","keepFiringFor":"0s","labels":{"severity":"warning"},"annotations":{},"enabled":true}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "update check rule",
			operation:      "update",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   string(expectedJSON),
			serverResponse: `{"status":"updated"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "delete check rule",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
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
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"error":"check rule not found"}`,
			serverStatus:   http.StatusNotFound,
			expectError:    true,
		},
		{
			name:           "create check rule - server error",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/alerting/check-rules/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   string(expectedJSON),
			serverResponse: `{"error":"internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
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
					assert.JSONEq(t, tc.expectedBody, string(bodyBytes))
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
				var checkRule *model.CheckRuleResourceModel
				checkRule, err = client.GetCheckRule(ctx, testDataset, testOrigin)
				if err == nil {
					assert.Equal(t, testOrigin, checkRule.Origin.ValueString())
					assert.Equal(t, testDataset, checkRule.Dataset.ValueString())
					assert.NotEmpty(t, checkRule.CheckRuleYaml.ValueString())
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
		response := map[string]any{
			"status": "success",
		}

		// Customize response based on the request
		switch r.URL.Path {
		case "/api/alerting/check-rules/test-check-rule":
			switch r.Method {
			case http.MethodGet:
				// Return a check rule JSON for GET requests
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"dataset":"default","name":"example-check-rules - HighMemoryUsage","expression":"memory_usage > 0.8","thresholds":{"degraded":0,"failed":0},"summary":"High memory usage detected","description":"","interval":"1m0s","for":"5m","keepFiringFor":"0s","labels":{"severity":"warning"},"annotations":{},"enabled":true}`))
				return
			case http.MethodDelete:
				response["status"] = "deleted"
			case http.MethodPut:
				response["status"] = "created_or_updated"
			}
		case "/api/alerting/check-rules/non-existent":
			status = http.StatusNotFound
			response = map[string]any{
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
metadata: {}
spec:
  groups:
    - name: example-check-rules
      interval: 1m0s
      rules:
        - alert: HighMemoryUsage
          expr: memory_usage > 0.8
          for: 5m
          annotations:
            summary: High memory usage detected
          labels:
            severity: warning`

	checkRuleModel := model.CheckRuleResourceModel{
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
		assert.Equal(t, "/api/alerting/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]any
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields (Dash0 format)
		assert.Equal(t, "example-check-rules - HighMemoryUsage", jsonObj["name"])
		assert.Equal(t, "memory_usage > 0.8", jsonObj["expression"])
		assert.Equal(t, "1m0s", jsonObj["interval"])
	})

	// 2. Get check rule
	t.Run("get check rule", func(t *testing.T) {
		checkRule, err := client.GetCheckRule(ctx, testDataset, testOrigin)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodGet, lastReq.Method)
		assert.Equal(t, "/api/alerting/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Check response parsing
		assert.Equal(t, testOrigin, checkRule.Origin.ValueString())
		assert.Equal(t, testDataset, checkRule.Dataset.ValueString())
		assert.NotEmpty(t, checkRule.CheckRuleYaml.ValueString())
		assert.Contains(t, checkRule.CheckRuleYaml.ValueString(), "HighMemoryUsage")
	})

	// 3. Update check rule
	t.Run("update check rule", func(t *testing.T) {
		// Update check rule YAML (but keep only one rule since converter only supports one)
		updatedYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: example-check-rules
      interval: 1m0s
      rules:
        - alert: HighCPUUsage
          expr: cpu_usage > 0.9
          for: 2m
          annotations:
            summary: High CPU usage detected
          labels:
            severity: critical`
		updatedModel := checkRuleModel
		updatedModel.CheckRuleYaml = types.StringValue(updatedYaml)

		err := client.UpdateCheckRule(ctx, updatedModel)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/alerting/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]any
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields (Dash0 format)
		assert.Equal(t, "example-check-rules - HighCPUUsage", jsonObj["name"])
		assert.Equal(t, "cpu_usage > 0.9", jsonObj["expression"])
	})

	// 4. Delete check rule
	t.Run("delete check rule", func(t *testing.T) {
		err := client.DeleteCheckRule(ctx, testOrigin, testDataset)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodDelete, lastReq.Method)
		assert.Equal(t, "/api/alerting/check-rules/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
	})

	// 5. Test error handling with non-existent check rule
	t.Run("get non-existent check rule", func(t *testing.T) {
		_, err := client.GetCheckRule(ctx, testDataset, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API error (404)")
	})
}

func TestCheckRuleClient_InvalidYAML(t *testing.T) {
	ctx := context.Background()
	client := newDash0Client("http://localhost", "test-token")

	checkRuleModel := model.CheckRuleResourceModel{
		Origin:        types.StringValue("test-origin"),
		Dataset:       types.StringValue("test-dataset"),
		CheckRuleYaml: types.StringValue("invalid: : : yaml"),
	}

	// Test create with invalid YAML
	err := client.CreateCheckRule(ctx, checkRuleModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing resource YAML")

	// Test update with invalid YAML
	err = client.UpdateCheckRule(ctx, checkRuleModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing resource YAML")
}

func TestCheckRuleClient_EmptyFields(t *testing.T) {
	ctx := context.Background()

	testYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: example-check-rules
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: test_metric > 0.5
          for: 1m
          labels: {}
          annotations: {}`

	tests := []struct {
		name    string
		model   model.CheckRuleResourceModel
		wantErr bool
	}{
		{
			name: "empty origin",
			model: model.CheckRuleResourceModel{
				Origin:        types.StringValue(""),
				Dataset:       types.StringValue("test-dataset"),
				CheckRuleYaml: types.StringValue(testYaml),
			},
			wantErr: false, // Should still work, just creates empty path
		},
		{
			name: "empty dataset",
			model: model.CheckRuleResourceModel{
				Origin:        types.StringValue("test-origin"),
				Dataset:       types.StringValue(""),
				CheckRuleYaml: types.StringValue(testYaml),
			},
			wantErr: false, // Should still work, just empty query param
		},
		{
			name: "empty YAML",
			model: model.CheckRuleResourceModel{
				Origin:        types.StringValue("test-origin"),
				Dataset:       types.StringValue("test-dataset"),
				CheckRuleYaml: types.StringValue(""),
			},
			wantErr: true, // Should fail during conversion
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a simple test server that always returns 200 OK
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"ok"}`))
			}))
			defer server.Close()

			client := newDash0Client(server.URL, "test-token")

			err := client.CreateCheckRule(ctx, tc.model)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckRuleClient_UnsupportedYAMLFormats(t *testing.T) {
	ctx := context.Background()
	client := newDash0Client("http://localhost", "test-token")

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "multiple groups",
			yaml: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: group1
      rules:
        - alert: Alert1
          expr: expr1
    - name: group2
      rules:
        - alert: Alert2
          expr: expr2`,
		},
		{
			name: "multiple rules in one group",
			yaml: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: group1
      rules:
        - alert: Alert1
          expr: expr1
        - alert: Alert2
          expr: expr2`,
		},
		{
			name: "missing required fields",
			yaml: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: group1
      rules:
        - expr: expr1`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkRuleModel := model.CheckRuleResourceModel{
				Origin:        types.StringValue("test-origin"),
				Dataset:       types.StringValue("test-dataset"),
				CheckRuleYaml: types.StringValue(tc.yaml),
			}

			err := client.CreateCheckRule(ctx, checkRuleModel)
			assert.Error(t, err)
		})
	}
}
