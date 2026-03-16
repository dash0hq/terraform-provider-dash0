package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

func (c *dash0Client) CreateRecordingRuleGroup(ctx context.Context, group model.RecordingRuleGroup) error {
	apiPath := "/api/recording-rule-groups"

	jsonBody, err := converter.ConvertYAMLToJSON(group.RecordingRuleGroupYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting recording rule group YAML to JSON: %w", err)
	}

	jsonBody, err = injectRecordingRuleGroupLabels(jsonBody, group.Dataset.ValueString(), group.Origin.ValueString())
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating Recording Rule Group with JSON payload: %s", jsonBody))

	// we are not using the helper because we don't send dataset as query param. it is mandatory in body.
	resp, err := c.doRequest(ctx, http.MethodPost, apiPath, jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording Rule Group created. Got API response: %s", resp))
	return nil
}

func (c *dash0Client) GetRecordingRuleGroup(ctx context.Context, dataset string, origin string) (*model.RecordingRuleGroup, error) {
	apiPath := fmt.Sprintf("/api/recording-rule-groups/%s", origin)
	resp, err := c.get(ctx, origin, dataset, apiPath, "Recording Rule Group")
	if err != nil {
		return nil, err
	}

	return &model.RecordingRuleGroup{
		Origin:                 types.StringValue(origin),
		Dataset:                types.StringValue(dataset),
		RecordingRuleGroupYaml: types.StringValue(string(resp)),
	}, nil
}

func (c *dash0Client) UpdateRecordingRuleGroup(ctx context.Context, group model.RecordingRuleGroup) error {
	apiPath := fmt.Sprintf("/api/recording-rule-groups/%s", group.Origin.ValueString())

	// Fetch the current version from the API (required for optimistic concurrency control)
	currentResp, err := c.get(ctx, group.Origin.ValueString(), group.Dataset.ValueString(), apiPath, "Recording Rule Group")
	if err != nil {
		return fmt.Errorf("error fetching current recording rule group for version: %w", err)
	}

	version, err := extractRecordingRuleGroupVersion(string(currentResp))
	if err != nil {
		return err
	}

	jsonBody, err := converter.ConvertYAMLToJSON(group.RecordingRuleGroupYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting recording rule group YAML to JSON: %w", err)
	}

	jsonBody, err = injectRecordingRuleGroupLabels(jsonBody, group.Dataset.ValueString(), group.Origin.ValueString())
	if err != nil {
		return err
	}

	jsonBody, err = injectRecordingRuleGroupVersion(jsonBody, version)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating Recording Rule Group with JSON payload: %s", jsonBody))

	// we are not using the helper because we don't send dataset as query param. it is mandatory in body.
	_, err = c.doRequest(ctx, http.MethodPut, apiPath, jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording Rule Group updated with origin: %s", group.Origin.ValueString()))
	return nil
}

func (c *dash0Client) DeleteRecordingRuleGroup(ctx context.Context, origin string, dataset string) error {
	apiPath := fmt.Sprintf("/api/recording-rule-groups/%s", origin)
	return c.delete(ctx, origin, dataset, apiPath, "Recording Rule Group")
}

// parseRecordingRuleGroupJSON unmarshals jsonStr and returns the top-level object plus its
// metadata.labels map. When orCreate is true, missing metadata/labels maps are created instead
// of returning an error.
func parseRecordingRuleGroupJSON(jsonStr string, orCreate bool) (map[string]interface{}, map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return nil, nil, fmt.Errorf("error parsing recording rule group JSON: %w", err)
	}

	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		if !orCreate {
			return nil, nil, fmt.Errorf("recording rule group response missing metadata")
		}
		metadata = make(map[string]interface{})
		obj["metadata"] = metadata
	}

	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		if !orCreate {
			return nil, nil, fmt.Errorf("recording rule group response missing metadata.labels")
		}
		labels = make(map[string]interface{})
		metadata["labels"] = labels
	}

	return obj, labels, nil
}

// extractRecordingRuleGroupVersion extracts the dash0.com/version label from a JSON response body.
func extractRecordingRuleGroupVersion(jsonStr string) (string, error) {
	_, labels, err := parseRecordingRuleGroupJSON(jsonStr, false)
	if err != nil {
		return "", err
	}

	version, ok := labels["dash0.com/version"]
	if !ok {
		return "", fmt.Errorf("recording rule group response missing dash0.com/version label")
	}

	return fmt.Sprintf("%v", version), nil
}

// injectRecordingRuleGroupVersion injects the dash0.com/version label into the JSON body for update requests.
func injectRecordingRuleGroupVersion(jsonStr string, version string) (string, error) {
	obj, labels, err := parseRecordingRuleGroupJSON(jsonStr, true)
	if err != nil {
		return "", err
	}

	labels["dash0.com/version"] = version

	result, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("error marshaling recording rule group JSON: %w", err)
	}
	return string(result), nil
}

// injectRecordingRuleGroupLabels injects the dash0.com/dataset and dash0.com/origin labels into the
// JSON body for create/update requests. The API expects dataset to be conveyed via metadata.labels
// rather than as a query parameter.
func injectRecordingRuleGroupLabels(jsonStr string, dataset, origin string) (string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", fmt.Errorf("error parsing recording rule group JSON: %w", err)
	}

	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
		obj["metadata"] = metadata
	}

	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		labels = make(map[string]interface{})
		metadata["labels"] = labels
	}

	labels["dash0.com/dataset"] = dataset
	labels["dash0.com/origin"] = origin

	result, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("error marshaling recording rule group JSON: %w", err)
	}
	return string(result), nil
}
