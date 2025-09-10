package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyntheticCheckClient_Operations(t *testing.T) {
	ctx := context.Background()
	testOrigin := "test-check"
	testDataset := "test-dataset"
	testYaml := `kind: Dash0SyntheticCheck
metadata:
  name: examplecom
  labels: {}
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com`

	// Convert YAML to expected JSON for requests
	expectedJSON, err := converter.ConvertYAMLToJSON(testYaml)
	require.NoError(t, err)

	checkModel := model.SyntheticCheck{
		Origin:             types.StringValue(testOrigin),
		Dataset:            types.StringValue(testDataset),
		SyntheticCheckYaml: types.StringValue(testYaml),
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
			name:           "Create synthetic check",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/synthetic-checks/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: "{}",
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Get synthetic check",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/synthetic-checks/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: testYaml,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Update synthetic check",
			operation:      "update",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/synthetic-checks/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: "{}",
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Delete synthetic check",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/synthetic-checks/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   "",
			serverResponse: "",
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Error response",
			operation:      "create",
			expectedMethod: http.MethodPut,
			expectedPath:   "/api/synthetic-checks/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			expectedBody:   expectedJSON,
			serverResponse: `{"error": "Internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, tt.expectedMethod, r.Method)
				assert.Equal(t, tt.expectedPath, r.URL.Path)
				assert.Equal(t, tt.expectedQuery, r.URL.RawQuery)

				if tt.expectedBody != "" {
					body := make([]byte, len(tt.expectedBody))
					_, err := r.Body.Read(body)
					assert.ErrorContains(t, err, "EOF")
					assert.JSONEq(t, tt.expectedBody, string(body))
				}

				// Send response
				w.WriteHeader(tt.serverStatus)
				if tt.serverResponse != "" {
					_, err := w.Write([]byte(tt.serverResponse))
					assert.NoError(t, err)
				}
			}))
			defer server.Close()

			// Create client
			client := NewDash0Client(server.URL, "test-token")

			// Execute operation
			var err error
			switch tt.operation {
			case "create":
				err = client.CreateSyntheticCheck(ctx, checkModel)
			case "get":
				var result *model.SyntheticCheck
				result, err = client.GetSyntheticCheck(ctx, testDataset, testOrigin)
				if !tt.expectError {
					assert.NotNil(t, result)
					assert.Equal(t, testOrigin, result.Origin.ValueString())
					assert.Equal(t, testDataset, result.Dataset.ValueString())
					assert.Equal(t, testYaml, result.SyntheticCheckYaml.ValueString())
				}
			case "update":
				err = client.UpdateSyntheticCheck(ctx, checkModel)
			case "delete":
				err = client.DeleteSyntheticCheck(ctx, testOrigin, testDataset)
			}

			// Verify error expectation
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSyntheticCheckClient_InvalidYAML(t *testing.T) {
	ctx := context.Background()
	client := NewDash0Client("http://localhost", "test-token")

	checkModel := model.SyntheticCheck{
		Origin:             types.StringValue("test-origin"),
		Dataset:            types.StringValue("test-dataset"),
		SyntheticCheckYaml: types.StringValue("invalid: : : yaml"),
	}

	// Test create with invalid YAML
	err := client.CreateSyntheticCheck(ctx, checkModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error converting synthetic check YAML to JSON")

	// Test update with invalid YAML
	err = client.UpdateSyntheticCheck(ctx, checkModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error converting synthetic check YAML to JSON")
}
