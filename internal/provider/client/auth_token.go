package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateAuthToken creates the auth token with the provided origin. Tokens are
// upserted by origin via PUT /api/auth-tokens/{origin}, so the call is
// idempotent under state loss. The server generates the id and the secret; both
// are returned. The caller must use an unrestricted "*" token.
func (c *dash0Client) CreateAuthToken(ctx context.Context, origin string, authTokenJSON string) (string, string, error) {
	def, err := unmarshalAuthToken(authTokenJSON)
	if err != nil {
		return "", "", fmt.Errorf("error parsing auth token JSON: %w", err)
	}

	setAuthTokenOrigin(def, origin)

	tflog.Debug(ctx, fmt.Sprintf("Creating auth token with origin: %s", origin))

	created, err := c.inner.UpdateAuthToken(ctx, origin, def)
	if err != nil {
		return "", "", err
	}

	// The API returns the secret only in the response that creates the token.
	id := dash0.GetAuthTokenID(created)
	var token string
	if created.Spec.Token != nil {
		token = *created.Spec.Token
	}
	if id == "" || token == "" {
		return "", "", fmt.Errorf("the API response for auth token %s has no id or token", origin)
	}

	tflog.Debug(ctx, fmt.Sprintf("Auth token created with origin: %s", origin))
	return id, token, nil
}

// GetAuthToken retrieves the auth token with the given id or origin. It returns
// the definition as JSON without the secret, so the secret never lands in the
// YAML state, and the secret separately. The API returns the secret only when
// it creates the token, so the secret is empty here unless the API says otherwise.
func (c *dash0Client) GetAuthToken(ctx context.Context, idOrOrigin string) (string, string, error) {
	def, err := c.inner.GetAuthToken(ctx, idOrOrigin)
	if err != nil {
		return "", "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Auth token retrieved: %s", idOrOrigin))

	var token string
	if def.Spec.Token != nil {
		token = *def.Spec.Token
	}

	// The request type has no secret field, so the conversion drops it.
	body, err := marshalToJSON(dash0.ConvertAuthTokenResponseToDefinition(def))
	if err != nil {
		return "", "", err
	}
	return body, token, nil
}

// UpdateAuthToken updates the auth token with the given id or origin using PUT.
// The request type has no secret field: PUT cannot change it. When origin is non-empty it
// is stamped onto dash0.com/origin; the server refuses to change an origin.
func (c *dash0Client) UpdateAuthToken(ctx context.Context, idOrOrigin string, origin string, authTokenJSON string) error {
	def, err := unmarshalAuthToken(authTokenJSON)
	if err != nil {
		return fmt.Errorf("error parsing auth token JSON: %w", err)
	}

	setAuthTokenOrigin(def, origin)

	if _, err = c.inner.UpdateAuthToken(ctx, idOrOrigin, def); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Auth token updated: %s", idOrOrigin))
	return nil
}

// DeleteAuthToken deletes the auth token with the given id or origin. The API
// refuses to delete the last auth token of the organization with a 400.
func (c *dash0Client) DeleteAuthToken(ctx context.Context, idOrOrigin string) error {
	if err := c.inner.DeleteAuthToken(ctx, idOrOrigin); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Auth token deleted: %s", idOrOrigin))
	return nil
}

func unmarshalAuthToken(jsonStr string) (*dash0.AuthTokenDefinition, error) {
	var def dash0.AuthTokenDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// setAuthTokenOrigin stamps origin into metadata.labels["dash0.com/origin"],
// initializing labels as needed. An empty origin leaves the definition as is.
func setAuthTokenOrigin(def *dash0.AuthTokenDefinition, origin string) {
	if origin == "" {
		return
	}
	if def.Metadata.Labels == nil {
		def.Metadata.Labels = &dash0.AuthTokenLabels{}
	}
	def.Metadata.Labels.Dash0Comorigin = &origin
}
