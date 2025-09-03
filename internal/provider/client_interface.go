package provider

import (
	"context"
)

type dash0ClientInterface interface {
	CreateDashboard(ctx context.Context, dashboard dashboardResourceModel) error
	GetDashboard(ctx context.Context, dataset string, origin string) (*dashboardResourceModel, error)
	UpdateDashboard(ctx context.Context, dashboard dashboardResourceModel) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error

	CreateSyntheticCheck(ctx context.Context, check syntheticCheckResourceModel) error
	GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*syntheticCheckResourceModel, error)
	UpdateSyntheticCheck(ctx context.Context, check syntheticCheckResourceModel) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error

	CreateView(ctx context.Context, check viewResourceModel) error
	GetView(ctx context.Context, dataset string, origin string) (*viewResourceModel, error)
	UpdateView(ctx context.Context, check viewResourceModel) error
	DeleteView(ctx context.Context, origin string, dataset string) error
}

// Ensure dash0Client implements dash0ClientInterface
var _ dash0ClientInterface = &dash0Client{}
