package client

import (
	"context"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

type Client interface {
	CreateDashboard(ctx context.Context, dashboard model.Dashboard) error
	GetDashboard(ctx context.Context, dataset string, origin string) (*model.Dashboard, error)
	UpdateDashboard(ctx context.Context, dashboard model.Dashboard) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error

	CreateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error
	GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*model.SyntheticCheck, error)
	UpdateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error

	CreateView(ctx context.Context, check model.ViewResource) error
	GetView(ctx context.Context, dataset string, origin string) (*model.ViewResource, error)
	UpdateView(ctx context.Context, check model.ViewResource) error
	DeleteView(ctx context.Context, origin string, dataset string) error

	CreateCheckRule(ctx context.Context, checkRule model.CheckRule) error
	GetCheckRule(ctx context.Context, dataset string, origin string) (*model.CheckRule, error)
	UpdateCheckRule(ctx context.Context, checkRule model.CheckRule) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error
}

// Ensure dash0Client implements dash0ClientInterface
var _ Client = &dash0Client{}
