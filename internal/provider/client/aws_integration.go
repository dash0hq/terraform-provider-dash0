package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

const awsIacIntegrationPath = "/public/aws/iac-integration"

func (c *dash0Client) CreateOrUpdateAwsIntegration(ctx context.Context, payload model.AwsIntegrationApiPayload) error {
	payload.Action = "create_or_update"
	payload.Source = "terraform"

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling AWS integration payload: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Registering AWS integration with Dash0 API: %s", string(body)))

	resp, err := c.doRequest(ctx, http.MethodPost, awsIacIntegrationPath, string(body))
	if err != nil {
		return fmt.Errorf("error registering AWS integration with Dash0: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("AWS integration registered successfully. Response: %s", string(resp)))
	return nil
}

func (c *dash0Client) DeleteAwsIntegration(ctx context.Context, sourceStateID string, externalID string) error {
	payload := model.AwsIntegrationApiPayload{
		Action:        "delete",
		Source:        "terraform",
		SourceStateID: sourceStateID,
		ExternalID:    externalID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling AWS integration delete payload: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Deleting AWS integration from Dash0 API: %s", string(body)))

	resp, err := c.doRequest(ctx, http.MethodPost, awsIacIntegrationPath, string(body))
	if err != nil {
		return fmt.Errorf("error deleting AWS integration from Dash0: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("AWS integration deleted successfully. Response: %s", string(resp)))
	return nil
}
