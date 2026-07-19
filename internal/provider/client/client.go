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
//
// The ResolveX methods translate a provider-generated origin into the
// server-assigned identifier (and, where applicable, the deep-link URL into
// the Dash0 web app). All are best-effort: when the asset cannot be located,
// they return empty strings and no error so callers can surface the result as
// optional metadata rather than failing the operation.
type Client interface {
	CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	GetDashboard(ctx context.Context, origin string, dataset string) (string, error)
	UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error
	DeleteDashboard(ctx context.Context, origin string, dataset string) error
	ResolveDashboard(ctx context.Context, origin string, dataset string) (string, string, error)

	CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error)
	UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error
	DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error
	ResolveSyntheticCheck(ctx context.Context, origin string, dataset string) (string, string, error)

	CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	GetView(ctx context.Context, origin string, dataset string) (string, error)
	UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error
	DeleteView(ctx context.Context, origin string, dataset string) error
	ResolveView(ctx context.Context, origin string, dataset string) (string, string, error)

	CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	GetCheckRule(ctx context.Context, origin string, dataset string) (string, error)
	UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error
	DeleteCheckRule(ctx context.Context, origin string, dataset string) error
	ResolveCheckRule(ctx context.Context, origin string, dataset string) (string, string, error)

	CreateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error
	GetRecordingRule(ctx context.Context, origin string, dataset string) (string, error)
	UpdateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error
	DeleteRecordingRule(ctx context.Context, origin string, dataset string) error
	// ResolveRecordingRule returns the server-assigned id of the recording rule
	// with the given origin (no deep-link URL — the Dash0 web app does not
	// expose a per-recording-rule page).
	ResolveRecordingRule(ctx context.Context, origin string, dataset string) (string, error)

	CreateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	GetNotificationChannel(ctx context.Context, origin string) (string, error)
	UpdateNotificationChannel(ctx context.Context, origin string, channelJSON string) error
	DeleteNotificationChannel(ctx context.Context, origin string) error
	ResolveNotificationChannel(ctx context.Context, origin string) (string, string, error)

	CreateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error
	GetSpamFilter(ctx context.Context, origin string, dataset string) (string, error)
	UpdateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error
	DeleteSpamFilter(ctx context.Context, origin string, dataset string) error
	// ResolveSpamFilter returns the server-assigned id of the spam filter with
	// the given origin (no deep-link URL — the Dash0 web app does not expose
	// a per-spam-filter page).
	ResolveSpamFilter(ctx context.Context, origin string, dataset string) (string, error)
}

// Ensure dash0Client implements Client
var _ Client = &dash0Client{}

// matchOriginID returns the server-assigned internal id of the list item whose
// origin (or, as a fallback, whose id) matches the given identifier, or an
// empty string when no item matches.
//
// The Dash0 web app addresses assets by their internal id, which the
// single-asset endpoints do not return (they only echo the identifier that was
// used to fetch them). The id is therefore resolved from the list endpoint. The
// resolver tries origin first, then falls back to matching the identifier
// against the list item's id — this covers assets originally created in the
// Dash0 UI, which carry no `dash0.com/origin` label. The API's GET/PUT
// endpoints accept either an origin or an id as the identifier, so both
// matching paths produce a consistent result: an origin-matched item's id is
// what the deep-link builder needs, and an id-matched item is already indexed
// by the value the caller passed in.
//
// The accessor extracts the (id, origin) pair from each list item type.
func matchOriginID[T any](items []*T, identifier string, accessor func(*T) (string, *string)) string {
	for _, item := range items {
		if item == nil {
			continue
		}
		id, itemOrigin := accessor(item)
		if itemOrigin != nil && *itemOrigin == identifier {
			return id
		}
		if id != "" && id == identifier {
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
