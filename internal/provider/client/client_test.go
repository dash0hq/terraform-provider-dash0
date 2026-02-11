package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequest(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		serverResponse string
		serverStatus   int
		expectError    bool
	}{
		{
			name:           "successful GET request",
			method:         http.MethodGet,
			path:           "/api/test",
			body:           "",
			serverResponse: `{"status":"ok"}`,
			serverStatus:   http.StatusOK,
			expectError:    false,
		},
		{
			name:           "successful POST request with body",
			method:         http.MethodPost,
			path:           "/api/test",
			body:           `{"key":"value"}`,
			serverResponse: `{"status":"created"}`,
			serverStatus:   http.StatusCreated,
			expectError:    false,
		},
		{
			name:           "error response",
			method:         http.MethodGet,
			path:           "/api/error",
			body:           "",
			serverResponse: `{"error":"not found"}`,
			serverStatus:   http.StatusNotFound,
			expectError:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "Dash0 Terraform Provider/test", r.Header.Get("User-Agent"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				// Verify request path
				assert.Equal(t, tc.path, r.URL.Path)
				assert.Equal(t, tc.method, r.Method)

				// Send response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.serverStatus)
				_, err := w.Write([]byte(tc.serverResponse))
				require.NoError(t, err)
			}))
			defer server.Close()

			c := NewDash0Client(server.URL, "test-token", "test")

			// Make request
			resp, err := c.doRequest(context.Background(), tc.method, tc.path, tc.body)

			// Assert results
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.serverResponse, string(resp))
			}
		})
	}
}
