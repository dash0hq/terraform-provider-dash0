package client

import (
	"context"
	"fmt"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (c *dash0Client) CreateView(ctx context.Context, check model.ViewResource) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", check.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.ViewYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting view YAML to JSON: %w", err)
	}

	return c.create(ctx, check.Dataset.ValueString(), apiPath, jsonBody, "View")
}

func (c *dash0Client) GetView(ctx context.Context, dataset string, origin string) (*model.ViewResource, error) {
	apiPath := fmt.Sprintf("/api/views/%s", origin)
	resp, err := c.get(ctx, origin, dataset, apiPath, "View")
	if err != nil {
		return nil, err
	}

	return &model.ViewResource{
		Origin:   types.StringValue(origin),
		Dataset:  types.StringValue(dataset),
		ViewYaml: types.StringValue(string(resp)),
	}, nil
}

func (c *dash0Client) UpdateView(ctx context.Context, check model.ViewResource) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", check.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.ViewYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting view YAML to JSON: %w", err)
	}

	return c.update(ctx, check.Origin.ValueString(), check.Dataset.ValueString(), apiPath, jsonBody, "View")
}
func (c *dash0Client) DeleteView(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", origin)
	return c.delete(ctx, origin, dataset, apiPath, "View")
}
