package provider

import (
	"context"
)

type dash0ClientInterface interface {
	CreateDashboard(ctx context.Context, dashboard dashboardResourceModel) error
	GetDashboard(ctx context.Context, dataset string, origin string) (*dashboardResourceModel, error)
	UpdateDashboard(ctx context.Context, dashboard dashboardResourceModel) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error
	
	CreateCheckRule(ctx context.Context, checkRule checkRuleResourceModel) error
	GetCheckRule(ctx context.Context, dataset string, origin string) (*checkRuleResourceModel, error)
	UpdateCheckRule(ctx context.Context, checkRule checkRuleResourceModel) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error
}

// Ensure dash0Client implements dash0ClientInterface
var _ dash0ClientInterface = &dash0Client{}
