package client

import (
	"context"
	"encoding/json"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// Client defines the interface for interacting with the Dash0 API.
// All methods use raw JSON strings for request/response bodies.
type Client interface {
	CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	GetDashboard(ctx context.Context, origin string, dataset string) (string, error)
	UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error

	CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error)
	UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error

	CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	GetView(ctx context.Context, origin string, dataset string) (string, error)
	UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	DeleteView(ctx context.Context, origin string, dataset string) error

	CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	GetCheckRule(ctx context.Context, origin string, dataset string) (string, error)
	UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error

	CreateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	GetNotificationChannel(ctx context.Context, origin string) (string, error)
	UpdateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	DeleteNotificationChannel(ctx context.Context, origin string) error
}

// Ensure dash0Client implements Client
var _ Client = &dash0Client{}

// marshalToJSON marshals a value to a JSON string.
func marshalToJSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalDashboard parses a JSON string into a DashboardDefinition.
func unmarshalDashboard(jsonStr string) (*dash0.DashboardDefinition, error) {
	var def dash0.DashboardDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// unmarshalSyntheticCheck parses a JSON string into a SyntheticCheckDefinition.
func unmarshalSyntheticCheck(jsonStr string) (*dash0.SyntheticCheckDefinition, error) {
	var def dash0.SyntheticCheckDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// unmarshalView parses a JSON string into a ViewDefinition.
func unmarshalView(jsonStr string) (*dash0.ViewDefinition, error) {
	var def dash0.ViewDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}
