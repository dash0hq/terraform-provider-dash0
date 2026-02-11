package client

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

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

func TestDashboardOperations(t *testing.T) {
	// Test dashboard data
	testOrigin := "test-dashboard"
	testDataset := "test-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"

	// Convert YAML to expected JSON for requests
	expectedJSON, err := converter.ConvertYAMLToJSON(testYaml)
	require.NoError(t, err)

	dashboardModel := model.Dashboard{
		Origin:        types.StringValue(testOrigin),
		Dataset:       types.StringValue(testDataset),
		DashboardYaml: types.StringValue(testYaml),
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
			name:           "create dashboard",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/dashboards/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get dashboard",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/dashboards/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: testYaml,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "update dashboard",
			operation:      "update",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/dashboards/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"updated"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "delete dashboard",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/dashboards/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"status":"deleted"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get dashboard - not found",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/dashboards/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"error":"dashboard not found"}`,
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
			client := NewDash0Client(server.URL, "test-token", "test")
			ctx := context.Background()
			var err error

			// Execute the operation based on the test case
			switch tc.operation {
			case "create":
				err = client.CreateDashboard(ctx, dashboardModel)
			case "get":
				var dashboard *model.Dashboard
				dashboard, err = client.GetDashboard(ctx, testDataset, testOrigin)
				if err == nil {
					assert.Equal(t, testOrigin, dashboard.Origin.ValueString())
					assert.Equal(t, testDataset, dashboard.Dataset.ValueString())
					assert.Equal(t, testYaml, dashboard.DashboardYaml.ValueString())
				}
			case "update":
				err = client.UpdateDashboard(ctx, dashboardModel)
			case "delete":
				err = client.DeleteDashboard(ctx, testOrigin, testDataset)
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

func TestDashboardOperations_IntegrationStyle(t *testing.T) {
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
		if r.URL.Path == "/api/dashboards/test-dashboard" {
			if r.Method == http.MethodGet {
				// Return a dashboard YAML for GET requests
				w.Header().Set("Content-Type", "application/yaml")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"))
				return
			} else if r.Method == http.MethodDelete {
				response["status"] = "deleted"
			} else if r.Method == http.MethodPut {
				response["status"] = "created_or_updated"
			}
		} else if r.URL.Path == "/api/dashboards/non-existent" {
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
	client := NewDash0Client(server.URL, "test-token", "test")

	// Test dashboard data
	testOrigin := "test-dashboard"
	testDataset := "test-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"

	// We don't need to check the exact JSON since we validate structure in the test

	dashboardModel := model.Dashboard{
		Origin:        types.StringValue(testOrigin),
		Dataset:       types.StringValue(testDataset),
		DashboardYaml: types.StringValue(testYaml),
	}

	// Execute a complete CRUD workflow
	ctx := context.Background()

	// 1. Create dashboard
	t.Run("create dashboard", func(t *testing.T) {
		err := client.CreateDashboard(ctx, dashboardModel)
		require.NoError(t, err)

		// Check last request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/dashboards/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields
		assert.Equal(t, "Dashboard", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
	})

	// 2. Get dashboard
	t.Run("get dashboard", func(t *testing.T) {
		dashboard, err := client.GetDashboard(ctx, testDataset, testOrigin)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodGet, lastReq.Method)
		assert.Equal(t, "/api/dashboards/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Check response parsing
		assert.Equal(t, testOrigin, dashboard.Origin.ValueString())
		assert.Equal(t, testDataset, dashboard.Dataset.ValueString())
		assert.Equal(t, testYaml, dashboard.DashboardYaml.ValueString())
	})

	// 3. Update dashboard
	t.Run("update dashboard", func(t *testing.T) {
		// Update dashboard YAML
		updatedYaml := testYaml + "\n  description: Updated dashboard"
		updatedModel := dashboardModel
		updatedModel.DashboardYaml = types.StringValue(updatedYaml)

		err := client.UpdateDashboard(ctx, updatedModel)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/dashboards/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields
		assert.Equal(t, "Dashboard", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
		assert.Contains(t, jsonObj["spec"].(map[string]interface{}), "description")
	})

	// 4. Delete dashboard
	t.Run("delete dashboard", func(t *testing.T) {
		err := client.DeleteDashboard(ctx, testOrigin, testDataset)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodDelete, lastReq.Method)
		assert.Equal(t, "/api/dashboards/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
	})

	// 5. Test error handling with non-existent dashboard
	t.Run("get non-existent dashboard", func(t *testing.T) {
		_, err := client.GetDashboard(ctx, testDataset, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API error (404)")
	})
}
