package client

import (
	"context"

	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
)

type Client interface {
	CreateDashboard(ctx context.Context, dashboard model.DashboardResourceModel) error
	GetDashboard(ctx context.Context, dataset string, origin string) (*model.DashboardResourceModel, error)
	UpdateDashboard(ctx context.Context, dashboard model.DashboardResourceModel) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error

	CreateSyntheticCheck(ctx context.Context, check model.SyntheticCheckResourceModel) error
	GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*model.SyntheticCheckResourceModel, error)
	UpdateSyntheticCheck(ctx context.Context, check model.SyntheticCheckResourceModel) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error

	CreateView(ctx context.Context, check model.ViewResourceModel) error
	GetView(ctx context.Context, dataset string, origin string) (*model.ViewResourceModel, error)
	UpdateView(ctx context.Context, check model.ViewResourceModel) error
	DeleteView(ctx context.Context, origin string, dataset string) error

	CreateCheckRule(ctx context.Context, checkRule model.CheckRuleResourceModel) error
	GetCheckRule(ctx context.Context, dataset string, origin string) (*model.CheckRuleResourceModel, error)
	UpdateCheckRule(ctx context.Context, checkRule model.CheckRuleResourceModel) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error
}

// Ensure dash0Client implements dash0ClientInterface
var _ Client = &dash0Client{}
