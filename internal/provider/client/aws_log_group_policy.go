package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// GetAwsLogGroupPolicy returns the log-group instrumentation policy of the AWS
// integration as JSON, with labels and annotations stripped, plus its
// dash0.com/origin label ("" when absent) and whether it is a deletion
// tombstone. An integration that never had a policy returns a not-found error.
func (c *dash0Client) GetAwsLogGroupPolicy(ctx context.Context, integrationID string) (string, string, bool, error) {
	policy, err := c.inner.GetAwsLogGroupInstrumentationPolicy(ctx, integrationID)
	if err != nil {
		return "", "", false, err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS log-group policy retrieved for integration: %s", integrationID))

	origin := dash0.GetAwsLogGroupInstrumentationPolicyOrigin(policy)
	deleted := dash0.IsAwsLogGroupInstrumentationPolicyDeleted(policy)
	dash0.StripAwsLogGroupInstrumentationPolicyServerFields(policy)
	policy.Metadata = dash0.AwsLogGroupPolicyMetadata{}
	if policy.Spec.OnNoLongerMatching == nil {
		keep := dash0.AwsLogGroupPolicyDispositionKeep
		policy.Spec.OnNoLongerMatching = &keep
	}
	policyJSON, err := marshalToJSON(policy)
	if err != nil {
		return "", "", false, err
	}
	return policyJSON, origin, deleted, nil
}

// UpdateAwsLogGroupPolicy creates or replaces the policy of the AWS
// integration with policyJSON, stamping origin on dash0.com/origin. An empty
// origin sends no label. No dash0.com/version is sent, so the write replaces
// whatever is stored.
func (c *dash0Client) UpdateAwsLogGroupPolicy(ctx context.Context, integrationID string, origin string, policyJSON string) error {
	policy, err := ParseAwsLogGroupPolicy(policyJSON)
	if err != nil {
		return err
	}
	policy.Metadata = dash0.AwsLogGroupPolicyMetadata{}
	if origin != "" {
		dash0.SetAwsLogGroupInstrumentationPolicyOrigin(policy, origin)
	}

	if _, err := c.inner.UpdateAwsLogGroupInstrumentationPolicy(ctx, integrationID, policy); err != nil {
		return err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS log-group policy written for integration: %s", integrationID))
	return nil
}

// ParseAwsLogGroupPolicy parses policyJSON and rejects a missing spec.rules,
// an unknown field in spec, and an unsupported operator or disposition. A
// missing rules list would reach the API as [] and match no log group.
func ParseAwsLogGroupPolicy(policyJSON string) (*dash0.AwsLogGroupInstrumentationPolicy, error) {
	var policy dash0.AwsLogGroupInstrumentationPolicy
	if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
		return nil, fmt.Errorf("error parsing AWS log-group policy JSON: %w", err)
	}
	if policy.Spec.Rules == nil {
		return nil, errors.New("spec.rules is required; use an empty list for a policy that matches no log group")
	}

	// metadata stays lenient: Dash0 manages it and the provider ignores it.
	var raw struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal([]byte(policyJSON), &raw); err != nil {
		return nil, fmt.Errorf("error parsing AWS log-group policy JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dash0.AwsLogGroupPolicySpec{}); err != nil {
		return nil, fmt.Errorf("invalid AWS log-group policy spec: %w", err)
	}

	if d := policy.Spec.OnNoLongerMatching; d != nil && !d.Valid() {
		return nil, fmt.Errorf("spec.onNoLongerMatching must be %q or %q, got %q", dash0.AwsLogGroupPolicyDispositionKeep, dash0.AwsLogGroupPolicyDispositionRemove, *d)
	}
	for i, rule := range policy.Spec.Rules {
		for j, condition := range rule.Conditions {
			if !condition.Operator.Valid() {
				return nil, fmt.Errorf("spec.rules[%d].conditions[%d].operator %q is not supported (supported: is, is_one_of, is_set, starts_with, ends_with, contains)", i, j, condition.Operator)
			}
		}
	}
	return &policy, nil
}

// DeleteAwsLogGroupPolicy deletes the policy of the AWS integration.
// disposition "keep" keeps forwarding the log groups the policy matched, and
// "remove" removes their subscription filters.
func (c *dash0Client) DeleteAwsLogGroupPolicy(ctx context.Context, integrationID string, disposition string) error {
	if _, err := c.inner.DeleteAwsLogGroupInstrumentationPolicy(ctx, integrationID, dash0.AwsLogGroupPolicyDisposition(disposition), nil); err != nil {
		return err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS log-group policy deleted for integration %s with disposition %s", integrationID, disposition))
	return nil
}
