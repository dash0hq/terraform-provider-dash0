// SPDX-FileCopyrightText: Copyright 2023-2026 Dash0 Inc.

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

func TestRecordingRuleGroupOperations(t *testing.T) {
	testOrigin := "test-recording-rule-group"
	testDataset := "test-dataset"
	testYaml := `kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics
  interval: 1m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production`

	// Convert YAML to expected JSON for requests
	expectedJSON, err := converter.ConvertYAMLToJSON(testYaml)
	require.NoError(t, err)

	groupModel := model.RecordingRuleGroup{
		Origin:                 types.StringValue(testOrigin),
		Dataset:                types.StringValue(testDataset),
		RecordingRuleGroupYaml: types.StringValue(testYaml),
	}

	tests := []struct {
		name           string
		operation      string
		expectedMethod string
		expectedPath   string
		expectedQuery  string
		serverResponse string
		serverStatus   int
		expectError    bool
		verifyBody     func(t *testing.T, body string)
	}{
		{
			name:           "create recording rule group",
			operation:      "create",
			expectedMethod: http.MethodPost,
			expectedPath:   "/api/recording-rule-groups",
			expectedQuery:  "",
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
			verifyBody: func(t *testing.T, body string) {
				// Verify JSON contains expected fields plus injected labels
				var obj map[string]interface{}
				err := json.Unmarshal([]byte(body), &obj)
				require.NoError(t, err)
				assert.Equal(t, "Dash0RecordingRuleGroup", obj["kind"])

				metadata := obj["metadata"].(map[string]interface{})
				labels := metadata["labels"].(map[string]interface{})
				assert.Equal(t, testDataset, labels["dash0.com/dataset"])
				assert.Equal(t, testOrigin, labels["dash0.com/origin"])
			},
		},
		{
			name:           "get recording rule group",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/recording-rule-groups/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			serverResponse: expectedJSON,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "update recording rule group",
			operation:      "update",
			expectedMethod: "", // Multiple requests: first GET then PUT
			expectedPath:   "/api/recording-rule-groups/" + testOrigin,
			expectedQuery:  "",
			serverResponse: "",
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "delete recording rule group",
			operation:      "delete",
			expectedMethod: http.MethodDelete,
			expectedPath:   "/api/recording-rule-groups/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			serverResponse: `{"status":"deleted"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "get recording rule group - not found",
			operation:      "get",
			expectedMethod: http.MethodGet,
			expectedPath:   "/api/recording-rule-groups/" + testOrigin,
			expectedQuery:  "dataset=" + testDataset,
			serverResponse: `{"error":"recording rule group not found"}`,
			serverStatus:   http.StatusNotFound,
			expectError:    true,
		},
		{
			name:           "create recording rule group - server error",
			operation:      "create",
			expectedMethod: http.MethodPost,
			expectedPath:   "/api/recording-rule-groups",
			expectedQuery:  "",
			serverResponse: `{"error":"internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.operation == "update" {
				// Update test needs special handling: it makes a GET (for version) then PUT
				testRecordingRuleGroupUpdate(t, testOrigin, testDataset, testYaml)
				return
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.expectedMethod, r.Method)
				assert.Equal(t, tc.expectedPath, r.URL.Path)
				if tc.expectedQuery != "" {
					assert.Equal(t, tc.expectedQuery, r.URL.RawQuery)
				}

				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				if tc.verifyBody != nil && r.Body != nil {
					bodyBytes, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					tc.verifyBody(t, string(bodyBytes))
				}

				w.WriteHeader(tc.serverStatus)
				if tc.serverResponse != "" {
					_, err := w.Write([]byte(tc.serverResponse))
					require.NoError(t, err)
				}
			}))
			defer server.Close()

			client := NewDash0Client(server.URL, "test-token", "test")
			ctx := context.Background()

			switch tc.operation {
			case "create":
				err = client.CreateRecordingRuleGroup(ctx, groupModel)
			case "get":
				var result *model.RecordingRuleGroup
				result, err = client.GetRecordingRuleGroup(ctx, testDataset, testOrigin)
				if !tc.expectError {
					assert.NotNil(t, result)
					assert.Equal(t, testOrigin, result.Origin.ValueString())
					assert.Equal(t, testDataset, result.Dataset.ValueString())
				}
			case "delete":
				err = client.DeleteRecordingRuleGroup(ctx, testOrigin, testDataset)
			}

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func testRecordingRuleGroupUpdate(t *testing.T, testOrigin, testDataset, testYaml string) {
	// The update flow: first GET (to fetch version), then PUT (with version injected)
	requestCount := 0

	// Build a JSON response with version for the GET
	getResponse := `{"kind":"Dash0RecordingRuleGroup","metadata":{"name":"http_metrics","labels":{"dash0.com/dataset":"` + testDataset + `","dash0.com/origin":"` + testOrigin + `","dash0.com/version":"42"}},"spec":{"enabled":true,"display":{"name":"HTTP Metrics"},"interval":"1m","rules":[{"record":"http_requests_total:rate5m","expression":"rate(http_requests_total[5m])","labels":{"env":"production"}}]}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// First request: GET for version
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/recording-rule-groups/"+testOrigin, r.URL.Path)
			assert.Equal(t, testDataset, r.URL.Query().Get("dataset"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(getResponse))
		} else {
			// Second request: PUT with version
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/api/recording-rule-groups/"+testOrigin, r.URL.Path)

			bodyBytes, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var obj map[string]interface{}
			err = json.Unmarshal(bodyBytes, &obj)
			require.NoError(t, err)

			// Verify version was injected
			metadata := obj["metadata"].(map[string]interface{})
			labels := metadata["labels"].(map[string]interface{})
			assert.Equal(t, "42", labels["dash0.com/version"])
			assert.Equal(t, testDataset, labels["dash0.com/dataset"])
			assert.Equal(t, testOrigin, labels["dash0.com/origin"])

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"updated"}`))
		}
	}))
	defer server.Close()

	client := NewDash0Client(server.URL, "test-token", "test")
	ctx := context.Background()

	groupModel := model.RecordingRuleGroup{
		Origin:                 types.StringValue(testOrigin),
		Dataset:                types.StringValue(testDataset),
		RecordingRuleGroupYaml: types.StringValue(testYaml),
	}

	err := client.UpdateRecordingRuleGroup(ctx, groupModel)
	require.NoError(t, err)
	assert.Equal(t, 2, requestCount, "Update should make exactly 2 requests (GET + PUT)")
}

func TestRecordingRuleGroupOperations_IntegrationStyle(t *testing.T) {
	var receivedRequests []*http.Request
	var receivedBodies []string

	getResponseJSON := `{"kind":"Dash0RecordingRuleGroup","metadata":{"name":"http_metrics","labels":{"dash0.com/dataset":"test-dataset","dash0.com/origin":"test-recording-rule-group","dash0.com/version":"1"}},"spec":{"enabled":true,"display":{"name":"HTTP Metrics"},"interval":"1m","rules":[{"record":"http_requests_total:rate5m","expression":"rate(http_requests_total[5m])","labels":{"env":"production"}}]}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequests = append(receivedRequests, r)

		if r.Body != nil {
			bodyBytes, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1048576))
			receivedBodies = append(receivedBodies, string(bodyBytes))
		} else {
			receivedBodies = append(receivedBodies, "")
		}

		if r.URL.Path == "/api/recording-rule-groups" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"created"}`))
			return
		}

		if r.URL.Path == "/api/recording-rule-groups/test-recording-rule-group" {
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(getResponseJSON))
			case http.MethodPut:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"updated"}`))
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"deleted"}`))
			}
			return
		}

		if r.URL.Path == "/api/recording-rule-groups/non-existent" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewDash0Client(server.URL, "test-token", "test")
	ctx := context.Background()

	testOrigin := "test-recording-rule-group"
	testDataset := "test-dataset"
	testYaml := "kind: Dash0RecordingRuleGroup\nmetadata:\n  name: http_metrics\nspec:\n  enabled: true\n  display:\n    name: HTTP Metrics\n  interval: 1m\n  rules:\n    - record: http_requests_total:rate5m\n      expression: rate(http_requests_total[5m])\n      labels:\n        env: production"

	groupModel := model.RecordingRuleGroup{
		Origin:                 types.StringValue(testOrigin),
		Dataset:                types.StringValue(testDataset),
		RecordingRuleGroupYaml: types.StringValue(testYaml),
	}

	// 1. Create
	t.Run("create recording rule group", func(t *testing.T) {
		err := client.CreateRecordingRuleGroup(ctx, groupModel)
		require.NoError(t, err)

		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodPost, lastReq.Method)
		assert.Equal(t, "/api/recording-rule-groups", lastReq.URL.Path)

		jsonBody := receivedBodies[len(receivedBodies)-1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(jsonBody), &jsonObj)
		assert.NoError(t, err, "Body should be valid JSON")
		assert.Equal(t, "Dash0RecordingRuleGroup", jsonObj["kind"])

		// Verify labels were injected
		metadata := jsonObj["metadata"].(map[string]interface{})
		labels := metadata["labels"].(map[string]interface{})
		assert.Equal(t, testDataset, labels["dash0.com/dataset"])
		assert.Equal(t, testOrigin, labels["dash0.com/origin"])
	})

	// 2. Get
	t.Run("get recording rule group", func(t *testing.T) {
		group, err := client.GetRecordingRuleGroup(ctx, testDataset, testOrigin)
		require.NoError(t, err)

		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodGet, lastReq.Method)
		assert.Equal(t, "/api/recording-rule-groups/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))

		assert.Equal(t, testOrigin, group.Origin.ValueString())
		assert.Equal(t, testDataset, group.Dataset.ValueString())
	})

	// 3. Update (makes GET + PUT)
	t.Run("update recording rule group", func(t *testing.T) {
		updatedYaml := testYaml + "\n    - record: http_errors_total:rate5m\n      expression: rate(http_requests_total{status=~\"5..\"}[5m])"
		updatedModel := groupModel
		updatedModel.RecordingRuleGroupYaml = types.StringValue(updatedYaml)

		reqCountBefore := len(receivedRequests)
		err := client.UpdateRecordingRuleGroup(ctx, updatedModel)
		require.NoError(t, err)

		// Should have made 2 requests: GET (for version) + PUT
		assert.Equal(t, reqCountBefore+2, len(receivedRequests))

		// First should be GET
		getReq := receivedRequests[reqCountBefore]
		assert.Equal(t, http.MethodGet, getReq.Method)

		// Second should be PUT
		putReq := receivedRequests[reqCountBefore+1]
		assert.Equal(t, http.MethodPut, putReq.Method)
		assert.Equal(t, "/api/recording-rule-groups/"+testOrigin, putReq.URL.Path)

		// Verify PUT body has version injected
		putBody := receivedBodies[reqCountBefore+1]
		var jsonObj map[string]interface{}
		err = json.Unmarshal([]byte(putBody), &jsonObj)
		require.NoError(t, err)
		metadata := jsonObj["metadata"].(map[string]interface{})
		labels := metadata["labels"].(map[string]interface{})
		assert.Equal(t, "1", labels["dash0.com/version"])
	})

	// 4. Delete
	t.Run("delete recording rule group", func(t *testing.T) {
		err := client.DeleteRecordingRuleGroup(ctx, testOrigin, testDataset)
		require.NoError(t, err)

		lastReq := receivedRequests[len(receivedRequests)-1]
		assert.Equal(t, http.MethodDelete, lastReq.Method)
		assert.Equal(t, "/api/recording-rule-groups/"+testOrigin, lastReq.URL.Path)
		assert.Equal(t, testDataset, lastReq.URL.Query().Get("dataset"))
	})

	// 5. Error handling
	t.Run("get non-existent recording rule group", func(t *testing.T) {
		_, err := client.GetRecordingRuleGroup(ctx, testDataset, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API error (404)")
	})
}

func TestRecordingRuleGroupClient_InvalidYAML(t *testing.T) {
	ctx := context.Background()
	client := NewDash0Client("http://localhost", "test-token", "test")

	groupModel := model.RecordingRuleGroup{
		Origin:                 types.StringValue("test-origin"),
		Dataset:                types.StringValue("test-dataset"),
		RecordingRuleGroupYaml: types.StringValue("invalid: : : yaml"),
	}

	// Test create with invalid YAML
	err := client.CreateRecordingRuleGroup(ctx, groupModel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error converting recording rule group YAML to JSON")
}

func TestInjectRecordingRuleGroupLabels(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		dataset  string
		origin   string
		wantErr  bool
		validate func(t *testing.T, result string)
	}{
		{
			name:    "injects labels into existing metadata",
			jsonStr: `{"kind":"Dash0RecordingRuleGroup","metadata":{"name":"test"},"spec":{}}`,
			dataset: "my-dataset",
			origin:  "tf_123",
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var obj map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(result), &obj))
				labels := obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
				assert.Equal(t, "my-dataset", labels["dash0.com/dataset"])
				assert.Equal(t, "tf_123", labels["dash0.com/origin"])
			},
		},
		{
			name:    "creates metadata and labels if missing",
			jsonStr: `{"kind":"Dash0RecordingRuleGroup","spec":{}}`,
			dataset: "my-dataset",
			origin:  "tf_123",
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var obj map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(result), &obj))
				labels := obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
				assert.Equal(t, "my-dataset", labels["dash0.com/dataset"])
				assert.Equal(t, "tf_123", labels["dash0.com/origin"])
			},
		},
		{
			name:    "invalid JSON",
			jsonStr: "not json",
			dataset: "my-dataset",
			origin:  "tf_123",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := injectRecordingRuleGroupLabels(tc.jsonStr, tc.dataset, tc.origin)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tc.validate(t, result)
			}
		})
	}
}

func TestExtractRecordingRuleGroupVersion(t *testing.T) {
	tests := []struct {
		name        string
		jsonStr     string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "extracts version from labels",
			jsonStr:     `{"metadata":{"labels":{"dash0.com/version":"42"}}}`,
			wantVersion: "42",
			wantErr:     false,
		},
		{
			name:    "missing metadata",
			jsonStr: `{"spec":{}}`,
			wantErr: true,
		},
		{
			name:    "missing labels",
			jsonStr: `{"metadata":{"name":"test"}}`,
			wantErr: true,
		},
		{
			name:    "missing version label",
			jsonStr: `{"metadata":{"labels":{"dash0.com/dataset":"test"}}}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			jsonStr: "not json",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version, err := extractRecordingRuleGroupVersion(tc.jsonStr)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantVersion, version)
			}
		})
	}
}

func TestInjectRecordingRuleGroupVersion(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		version  string
		wantErr  bool
		validate func(t *testing.T, result string)
	}{
		{
			name:    "injects version into existing labels",
			jsonStr: `{"metadata":{"labels":{"dash0.com/dataset":"test"}}}`,
			version: "5",
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var obj map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(result), &obj))
				labels := obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
				assert.Equal(t, "5", labels["dash0.com/version"])
				assert.Equal(t, "test", labels["dash0.com/dataset"])
			},
		},
		{
			name:    "creates labels if missing",
			jsonStr: `{"metadata":{"name":"test"}}`,
			version: "1",
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var obj map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(result), &obj))
				labels := obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
				assert.Equal(t, "1", labels["dash0.com/version"])
			},
		},
		{
			name:    "invalid JSON",
			jsonStr: "not json",
			version: "1",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := injectRecordingRuleGroupVersion(tc.jsonStr, tc.version)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tc.validate(t, result)
			}
		})
	}
}
