package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateCustomSkill creates the Agent0 custom skill with the provided origin.
// Skills are upserted by origin via PUT /api/agent0/custom-skills/{origin}, so
// the call is idempotent under state loss. It returns the server-assigned id.
func (c *dash0Client) CreateCustomSkill(ctx context.Context, origin string, skillJSON string) (string, error) {
	def, err := unmarshalCustomSkill(skillJSON)
	if err != nil {
		return "", fmt.Errorf("error parsing custom skill JSON: %w", err)
	}

	// The API rejects a client-supplied id on create.
	dash0.ClearCustomSkillID(def)
	dash0.SetCustomSkillOrigin(def, origin)

	tflog.Debug(ctx, fmt.Sprintf("Creating custom skill with origin: %s", origin))

	created, err := c.inner.UpdateCustomSkill(ctx, origin, def)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Custom skill created with origin: %s", origin))
	return dash0.GetCustomSkillID(created), nil
}

// GetCustomSkill retrieves the custom skill with the given origin or id. It
// returns the definition as JSON with the fields the server sets or derives
// removed (see dash0.StripCustomSkillServerFields), keeping the dash0.com/id
// and dash0.com/origin labels and a dash0.com/deleted-at annotation.
func (c *dash0Client) GetCustomSkill(ctx context.Context, originOrID string) (string, error) {
	def, err := c.inner.GetCustomSkill(ctx, originOrID)
	if err != nil {
		return "", err
	}

	// Kept so that a soft-deleted skill is still recognized as gone.
	var deletedAt *dash0.SkillAnnotations
	if def.Metadata.Annotations != nil && def.Metadata.Annotations.Dash0ComdeletedAt != nil {
		deletedAt = &dash0.SkillAnnotations{Dash0ComdeletedAt: def.Metadata.Annotations.Dash0ComdeletedAt}
	}
	dash0.StripCustomSkillServerFields(def)
	def.Metadata.Annotations = deletedAt

	tflog.Debug(ctx, fmt.Sprintf("Custom skill retrieved: %s", originOrID))
	return marshalToJSON(def)
}

// UpdateCustomSkill replaces the custom skill with the given origin or id
// using PUT. The dash0.com/version label is dropped so that the write is not
// rejected as superseded: Terraform applies desired state.
func (c *dash0Client) UpdateCustomSkill(ctx context.Context, originOrID string, skillJSON string) error {
	def, err := unmarshalCustomSkill(skillJSON)
	if err != nil {
		return fmt.Errorf("error parsing custom skill JSON: %w", err)
	}

	if def.Metadata.Labels != nil {
		def.Metadata.Labels.Dash0Comversion = nil
	}

	if _, err = c.inner.UpdateCustomSkill(ctx, originOrID, def); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Custom skill updated: %s", originOrID))
	return nil
}

// DeleteCustomSkill deletes the custom skill with the given origin or id.
func (c *dash0Client) DeleteCustomSkill(ctx context.Context, originOrID string) error {
	if err := c.inner.DeleteCustomSkill(ctx, originOrID); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Custom skill deleted: %s", originOrID))
	return nil
}

// CustomSkillIdentity returns the dash0.com/id and dash0.com/origin labels of
// a custom skill JSON document, or empty strings when they are absent or the
// document cannot be parsed.
func CustomSkillIdentity(skillJSON string) (id string, origin string) {
	def, err := unmarshalCustomSkill(skillJSON)
	if err != nil {
		return "", ""
	}
	return dash0.GetCustomSkillID(def), dash0.GetCustomSkillOrigin(def)
}

func unmarshalCustomSkill(jsonStr string) (*dash0.SkillDefinition, error) {
	var def dash0.SkillDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}
