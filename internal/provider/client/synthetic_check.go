package client

import (
	"context"
	"fmt"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (c *dash0Client) CreateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", check.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.SyntheticCheckYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting synthetic check YAML to JSON: %w", err)
	}

	return c.create(ctx, check.Dataset.ValueString(), apiPath, jsonBody, "Synthetic check")
}

func (c *dash0Client) GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*model.SyntheticCheck, error) {
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", origin)
	resp, err := c.get(ctx, origin, dataset, apiPath, "Synthetic check")
	if err != nil {
		return nil, err
	}

	return &model.SyntheticCheck{
		Origin:             types.StringValue(origin),
		Dataset:            types.StringValue(dataset),
		SyntheticCheckYaml: types.StringValue(string(resp)),
	}, nil
}

func (c *dash0Client) UpdateSyntheticCheck(ctx context.Context, check model.SyntheticCheck) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", check.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.SyntheticCheckYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting synthetic check YAML to JSON: %w", err)
	}

	return c.update(ctx, check.Origin.ValueString(), check.Dataset.ValueString(), apiPath, jsonBody, "Synthetic check")
}

func (c *dash0Client) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", origin)
	return c.delete(ctx, origin, dataset, apiPath, "Synthetic check")
}
