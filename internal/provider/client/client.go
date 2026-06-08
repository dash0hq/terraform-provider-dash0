package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// Client defines the interface for interacting with the Dash0 API.
// All methods use raw JSON strings for request/response bodies.
type Client interface {
	CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	GetDashboard(ctx context.Context, origin string, dataset string) (string, error)
	UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error
	// GetDashboardURL returns a deep link to the Dash0 web app for the dashboard
	// with the given origin, or an empty string if it cannot be determined.
	GetDashboardURL(ctx context.Context, origin string, dataset string) (string, error)

	CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error)
	UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error
	// GetSyntheticCheckURL returns a deep link to the Dash0 web app for the
	// synthetic check with the given origin, or an empty string if it cannot be
	// determined.
	GetSyntheticCheckURL(ctx context.Context, origin string, dataset string) (string, error)

	CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	GetView(ctx context.Context, origin string, dataset string) (string, error)
	UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	DeleteView(ctx context.Context, origin string, dataset string) error
	// GetViewURL returns a deep link to the Dash0 web app for the view with the
	// given origin, or an empty string if it cannot be determined.
	GetViewURL(ctx context.Context, origin string, dataset string) (string, error)

	CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	GetCheckRule(ctx context.Context, origin string, dataset string) (string, error)
	UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error
	// GetCheckRuleURL returns a deep link to the Dash0 web app for the check rule
	// with the given origin, or an empty string if it cannot be determined.
	GetCheckRuleURL(ctx context.Context, origin string, dataset string) (string, error)

	CreateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error
	GetRecordingRule(ctx context.Context, origin string, dataset string) (string, error)
	UpdateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error
	DeleteRecordingRule(ctx context.Context, origin string, dataset string) error

	CreateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	GetNotificationChannel(ctx context.Context, origin string) (string, error)
	UpdateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	DeleteNotificationChannel(ctx context.Context, origin string) error
	// GetNotificationChannelURL returns a deep link to the Dash0 web app for the
	// notification channel with the given origin, or an empty string if it cannot
	// be determined.
	GetNotificationChannelURL(ctx context.Context, origin string) (string, error)
	// GetNotificationChannelID returns the server-assigned id (the bare
	// dash0.com/id UUID) of the notification channel with the given origin. This
	// id is the value other resources (synthetic checks, check rules) use to
	// reference the channel in their notification settings. It returns an empty
	// string if the id cannot be determined.
	GetNotificationChannelID(ctx context.Context, origin string) (string, error)

	CreateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error
	GetSpamFilter(ctx context.Context, origin string, dataset string) (string, error)
	UpdateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error
	DeleteSpamFilter(ctx context.Context, origin string, dataset string) error
}

// Ensure dash0Client implements Client
var _ Client = &dash0Client{}

// matchOriginID returns the server-assigned internal id of the list item whose
// origin matches the given origin, or an empty string when no item matches.
//
// The Dash0 web app addresses assets by their internal id, which the
// single-asset endpoints do not return (they only echo the origin). The id is
// therefore resolved from the list endpoint by matching on origin. The accessor
// extracts the (id, origin) pair from each list item type.
func matchOriginID[T any](items []*T, origin string, accessor func(*T) (string, *string)) string {
	for _, item := range items {
		if item == nil {
			continue
		}
		id, itemOrigin := accessor(item)
		if itemOrigin != nil && *itemOrigin == origin {
			return id
		}
	}
	return ""
}

// logResolvedURL emits a debug log for a resolved deep link, mirroring the
// logging done by the per-asset URL resolvers.
func logResolvedURL(ctx context.Context, assetType, origin, resolvedURL string) {
	tflog.Debug(ctx, fmt.Sprintf("Resolved %s URL for origin %s: %s", assetType, origin, resolvedURL))
}

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
