package client

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

func TestViewOperations(t *testing.T) {
	// Test view data
	testOrigin := "test-view"
	testDataset := "test-dataset"
	testYaml := `kind: View
metadata:
  name: example-view
spec:
  title: Example View`

	// Convert YAML to expected JSON for requests
	expectedJSON, err := converter.ConvertYAMLToJSON(testYaml)
	require.NoError(t, err)

	viewModel := model.ViewResource{
		Origin:   types.StringValue(testOrigin),
		Dataset:  types.StringValue(testDataset),
		ViewYaml: types.StringValue(testYaml),
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
			name:           "create view",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/views/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get view",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/views/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: testYaml,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "update view",
			operation:      "update",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/views/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"status":"updated"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "delete view",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/views/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"status":"deleted"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get view - not found",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/views/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: `{"error":"view not found"}`,
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
			client := NewDash0Client(server.URL, "test-token")
			ctx := context.Background()
			var err error

			// Execute the operation based on the test case
			switch tc.operation {
			case "create":
				err = client.CreateView(ctx, viewModel)
			case "get":
				var view *model.ViewResource
				view, err = client.GetView(ctx, testDataset, testOrigin)
				if err == nil {
					assert.Equal(t, testOrigin, view.Origin.ValueString())
					assert.Equal(t, testDataset, view.Dataset.ValueString())
					assert.Equal(t, testYaml, view.ViewYaml.ValueString())
				}
			case "update":
				err = client.UpdateView(ctx, viewModel)
			case "delete":
				err = client.DeleteView(ctx, testOrigin, testDataset)
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

func TestViewOperations_IntegrationStyle(t *testing.T) {
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
		if r.URL.Path == "/api/views/test-view" {
			if r.Method == http.MethodGet {
				// Return a view YAML for GET requests
				w.Header().Set("Content-Type", "application/yaml")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"))
				return
			} else if r.Method == http.MethodDelete {
				response["status"] = "deleted"
			} else if r.Method == http.MethodPut {
				response["status"] = "created_or_updated"
			}
		} else if r.URL.Path == "/api/views/non-existent" {
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
	client := NewDash0Client(server.URL, "test-token")

	// Test view data
	testOrigin := "test-view"
	testDataset := "test-dataset"
	testYaml := "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"

	viewModel := model.ViewResource{
		Origin:   types.StringValue(testOrigin),
		Dataset:  types.StringValue(testDataset),
		ViewYaml: types.StringValue(testYaml),
	}

	// Execute a complete CRUD workflow
	ctx := context.Background()

	// 1. Create view
	t.Run("create view", func(t *testing.T) {
		err := client.CreateView(ctx, viewModel)
		require.NoError(t, err)

		// Check last request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/views/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields
		assert.Equal(t, "View", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
	})

	// 2. Get view
	t.Run("get view", func(t *testing.T) {
		view, err := client.GetView(ctx, testDataset, testOrigin)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodGet, lastReq.Method)
		assert.Equal(t, "/api/views/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Check response parsing
		assert.Equal(t, testOrigin, view.Origin.ValueString())
		assert.Equal(t, testDataset, view.Dataset.ValueString())
		assert.Equal(t, testYaml, view.ViewYaml.ValueString())
	})

	// 3. Update view
	t.Run("update view", func(t *testing.T) {
		// Update view YAML
		updatedYaml := testYaml + "\n  description: Updated view"
		updatedModel := viewModel
		updatedModel.ViewYaml = types.StringValue(updatedYaml)

		err := client.UpdateView(ctx, updatedModel)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPut, lastReq.Method)
		assert.Equal(t, "/api/views/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		// Verify the request body is valid JSON (converted from YAML)
		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")

		// Verify JSON contains expected fields
		assert.Equal(t, "View", jsonObj["kind"])
		assert.Contains(t, jsonObj, "metadata")
		assert.Contains(t, jsonObj, "spec")
		assert.Contains(t, jsonObj["spec"].(map[string]interface{}), "description")
	})

	// 4. Delete view
	t.Run("delete view", func(t *testing.T) {
		err := client.DeleteView(ctx, testOrigin, testDataset)
		require.NoError(t, err)

		// Check request
		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodDelete, lastReq.Method)
		assert.Equal(t, "/api/views/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
	})

	// 5. Test error handling with non-existent view
	t.Run("get non-existent view", func(t *testing.T) {
		_, err := client.GetView(ctx, testDataset, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API error (404)")
	})
}

func TestViewClient_InvalidYAML(t *testing.T) {
	ctx := context.Background()
	client := NewDash0Client("http://localhost", "test-token")

	viewModel := model.ViewResource{
		Origin:   types.StringValue("test-origin"),
		Dataset:  types.StringValue("test-dataset"),
		ViewYaml: types.StringValue("invalid: : : yaml"),
	}

	// Test create with invalid YAML
	err := client.CreateView(ctx, viewModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error converting view YAML to JSON")

	// Test update with invalid YAML
	err = client.UpdateView(ctx, viewModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error converting view YAML to JSON")
}
