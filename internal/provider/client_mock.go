package provider

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

// MockClient mocks the client.Client for synthetic checks
type MockClient struct {
	mock.Mock
}

func (m *MockClient) CreateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	args := m.Called(ctx, dashboard)
	return args.Error(0)
}

func (m *MockClient) GetDashboard(ctx context.Context, dataset string, origin string) (*model.Dashboard, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Dashboard), args.Error(1)
}

func (m *MockClient) UpdateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	args := m.Called(ctx, dashboard)
	return args.Error(0)
}

func (m *MockClient) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*model.SyntheticCheck, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SyntheticCheck), args.Error(1)
}

func (m *MockClient) UpdateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateView(ctx context.Context, check model.ViewResource) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) GetView(ctx context.Context, dataset string, origin string) (*model.ViewResource, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.ViewResource), args.Error(1)
}

func (m *MockClient) UpdateView(ctx context.Context, check model.ViewResource) error {
	args := m.Called(ctx, check)
	return args.Error(0)
}

func (m *MockClient) DeleteView(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}
func (m *MockClient) CreateCheckRule(ctx context.Context, checkRule model.CheckRule) error {
	args := m.Called(ctx, checkRule)
	return args.Error(0)
}

func (m *MockClient) GetCheckRule(ctx context.Context, dataset string, origin string) (*model.CheckRule, error) {
	args := m.Called(ctx, dataset, origin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.CheckRule), args.Error(1)
}

func (m *MockClient) UpdateCheckRule(ctx context.Context, checkRule model.CheckRule) error {
	args := m.Called(ctx, checkRule)
	return args.Error(0)
}

func (m *MockClient) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}
