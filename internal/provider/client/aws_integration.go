package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

func (c *dash0Client) CreateOrUpdateAwsIntegration(ctx context.Context, integration model.AwsIntegration, accountID string) error {
	origin := model.AwsIntegrationOrigin(accountID, integration.ExternalID.ValueString())
	apiPath := fmt.Sprintf("/api/integrations/%s", origin)

	definition := model.BuildAwsIntegrationDefinition(integration, accountID)
	body, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("error marshaling AWS integration: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating/updating AWS integration with origin %s", origin))
	return c.create(ctx, integration.Dataset.ValueString(), apiPath, string(body), "AwsIntegration")
}

func (c *dash0Client) GetAwsIntegration(ctx context.Context, dataset, accountID, externalID string) (*model.AwsIntegrationSpec, error) {
	origin := model.AwsIntegrationOrigin(accountID, externalID)
	apiPath := fmt.Sprintf("/api/integrations/%s", origin)

	resp, err := c.get(ctx, origin, dataset, apiPath, "AwsIntegration")
	if err != nil {
		return nil, err
	}

	var definition model.IntegrationDefinition
	if err := json.Unmarshal(resp, &definition); err != nil {
		return nil, fmt.Errorf("error parsing AWS integration response: %w", err)
	}

	return &definition.Spec.Integration.Spec, nil
}

func (c *dash0Client) DeleteAwsIntegration(ctx context.Context, dataset, accountID, externalID string) error {
	origin := model.AwsIntegrationOrigin(accountID, externalID)
	apiPath := fmt.Sprintf("/api/integrations/%s", origin)

	tflog.Debug(ctx, fmt.Sprintf("Deleting AWS integration with origin %s", origin))
	return c.delete(ctx, origin, dataset, apiPath, "AwsIntegration")
}
