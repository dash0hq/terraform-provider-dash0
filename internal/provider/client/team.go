package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateTeam creates or replaces the team with the provided origin. Teams are
// upserted by origin via PUT /api/teams/{origin} so operations are idempotent
// under state loss — the same pattern used for notification channels.
//
// The provided JSON is decoded into a TeamDefinitionV1Alpha1, then the origin
// label is stamped onto metadata.labels so the server records the caller's
// origin instead of assigning a synthetic one.
func (c *dash0Client) CreateTeam(ctx context.Context, origin string, teamJSON string) error {
	def, err := unmarshalTeam(teamJSON)
	if err != nil {
		return fmt.Errorf("error parsing team JSON: %w", err)
	}

	setTeamOrigin(def, origin)

	tflog.Debug(ctx, fmt.Sprintf("Creating team with origin: %s", origin))

	_, err = c.inner.UpsertTeam(ctx, origin, def)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Team created with origin: %s", origin))
	return nil
}

// GetTeam retrieves the team with the given origin. Before returning the
// definition as JSON, member internal IDs are rewritten to email addresses via
// the shared ResolveTeamMembersToEmails helper so state normalization can
// compare against user-authored YAML that references members by email.
// Server-managed labels and annotations (id, source, created-at, updated-at)
// are stripped so drift detection ignores fields the API produced.
func (c *dash0Client) GetTeam(ctx context.Context, origin string) (string, error) {
	def, err := c.inner.GetTeam(ctx, origin)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Team retrieved with origin: %s", origin))

	// Rewrite spec.members to emails using the shared helper so the state
	// comparison lines up with user-authored YAML that lists members by email.
	// Best-effort: on failure, log-and-continue with the raw IDs. The helper
	// itself falls back to the raw ID for entries that don't resolve to an
	// email.
	if resolveErr := dash0.ResolveTeamMembersToEmails(ctx, c.inner, def); resolveErr != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to resolve team members to emails for origin %s: %s", origin, resolveErr))
	}

	// Strip server-managed fields so the read response does not perpetually
	// drift against user-authored YAML. dash0.com/origin is preserved by
	// StripTeamServerFields because it is client-settable on create and
	// immutable thereafter — normalization treats it as a regular label.
	dash0.StripTeamServerFields(def)

	return marshalToJSON(def)
}

// UpdateTeam updates the team with the given origin. Uses the same PUT
// endpoint as CreateTeam.
func (c *dash0Client) UpdateTeam(ctx context.Context, origin string, teamJSON string) error {
	def, err := unmarshalTeam(teamJSON)
	if err != nil {
		return fmt.Errorf("error parsing team JSON: %w", err)
	}

	setTeamOrigin(def, origin)

	_, err = c.inner.UpsertTeam(ctx, origin, def)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Team updated with origin: %s", origin))
	return nil
}

// DeleteTeam deletes the team identified by origin.
func (c *dash0Client) DeleteTeam(ctx context.Context, origin string) error {
	err := c.inner.DeleteTeam(ctx, origin)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Team deleted with origin: %s", origin))
	return nil
}

// ResolveTeam looks up the server-assigned id for the team with the given
// origin. Best-effort: returns an empty id and no error when the team cannot
// be located, so callers surface the id as optional metadata.
func (c *dash0Client) ResolveTeam(ctx context.Context, origin string) (string, error) {
	def, err := c.inner.GetTeam(ctx, origin)
	if err != nil {
		return "", err
	}
	if def == nil {
		return "", nil
	}
	return dash0.GetTeamID(def), nil
}

// unmarshalTeam parses a JSON string into a TeamDefinitionV1Alpha1.
func unmarshalTeam(jsonStr string) (*dash0.TeamDefinitionV1Alpha1, error) {
	var def dash0.TeamDefinitionV1Alpha1
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// setTeamOrigin stamps the provided origin into
// metadata.labels["dash0.com/origin"], initializing the labels struct as
// needed. This mirrors the write-path convention used across other
// origin-scoped resources.
func setTeamOrigin(def *dash0.TeamDefinitionV1Alpha1, origin string) {
	if def == nil {
		return
	}
	if def.Metadata.Labels == nil {
		def.Metadata.Labels = &dash0.TeamLabels{}
	}
	o := origin
	def.Metadata.Labels.Dash0Comorigin = &o
}
