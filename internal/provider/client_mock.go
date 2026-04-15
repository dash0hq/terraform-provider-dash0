package provider

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockClient mocks the client.Client interface
type MockClient struct {
	mock.Mock
}

func (m *MockClient) CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	args := m.Called(ctx, origin, dashboardJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetDashboard(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	args := m.Called(ctx, origin, dashboardJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	args := m.Called(ctx, origin, checkJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	args := m.Called(ctx, origin, checkJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	args := m.Called(ctx, origin, viewJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetView(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	args := m.Called(ctx, origin, viewJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteView(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	args := m.Called(ctx, origin, ruleYAML, dataset)
	return args.Error(0)
}

func (m *MockClient) GetCheckRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	args := m.Called(ctx, origin, ruleYAML, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}
