package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockClient mocks the dash0ClientInterface for synthetic checks
type MockClient struct {
	mock.Mock
}

func (m *MockClient) CreateDashboard(ctx context.Context, dashboard dashboardResourceModel) error {
	args := m.Called(ctx, dashboard)
	return args.Error(0)
}

func (m *MockClient) GetDashboard(ctx context.Context, dataset string, origin string) (*dashboardResourceModel, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dashboardResourceModel), args.Error(1)
}

func (m *MockClient) UpdateDashboard(ctx context.Context, dashboard dashboardResourceModel) error {
	args := m.Called(ctx, dashboard)
	return args.Error(0)
}

func (m *MockClient) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateSyntheticCheck(ctx context.Context, check syntheticCheckResourceModel) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*syntheticCheckResourceModel, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*syntheticCheckResourceModel), args.Error(1)
}

func (m *MockClient) UpdateSyntheticCheck(ctx context.Context, check syntheticCheckResourceModel) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateView(ctx context.Context, check viewResourceModel) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) GetView(ctx context.Context, dataset string, origin string) (*viewResourceModel, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*viewResourceModel), args.Error(1)
}

func (m *MockClient) UpdateView(ctx context.Context, check viewResourceModel) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) DeleteView(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

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
				assert.Equal(t, "Dash0 Terraform Provider", r.Header.Get("User-Agent"))
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

			// Create client
			client := newDash0Client(server.URL, "test-token")

			// Make request
			resp, err := client.doRequest(context.Background(), tc.method, tc.path, tc.body)

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
