package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateEval creates a generative AI evaluation under the
// given origin.
//
// Creation goes through PUT, not POST, which matches the provider's
// create-or-replace convention: PUT-to-origin is idempotent, so a retried
// create cannot produce a second evaluation.
func (c *dash0Client) CreateEval(ctx context.Context, origin string, evaluationJSON string, dataset string) error {
	if err := c.putEval(ctx, origin, evaluationJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Generative AI evaluation created with origin: %s", origin))
	return nil
}

// GetEval retrieves the generative AI evaluation with the
// given origin as a JSON document.
//
// Deletes are soft: after a delete, this returns the tombstone document
// (carrying a dash0.com/deleted-at annotation) rather than a 404. The resource
// layer owns the gone-detection branch.
func (c *dash0Client) GetEval(ctx context.Context, origin string, dataset string) (string, error) {
	evaluation, err := c.inner.GetGenerativeAiEvaluation(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Generative AI evaluation retrieved with origin: %s", origin))
	return marshalToJSON(evaluation)
}

// UpdateEval replaces the generative AI evaluation with the
// given origin. It is identical to CreateEval, because PUT is
// create-or-replace.
func (c *dash0Client) UpdateEval(ctx context.Context, origin string, evaluationJSON string, dataset string) error {
	if err := c.putEval(ctx, origin, evaluationJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Generative AI evaluation updated with origin: %s", origin))
	return nil
}

// putEval sends the evaluation with PUT to the given origin.
//
// The server-managed labels and annotations are stripped first, so a document
// copied from the API (the documented import flow) does not carry a stale
// dash0.com/version, which the API rejects as superseded, or an id that
// belongs to another evaluation. The dataset and origin labels are set to the
// resource's dataset and origin, so a copied document cannot contradict the
// request URL.
func (c *dash0Client) putEval(ctx context.Context, origin string, evaluationJSON string, dataset string) error {
	var evaluation dash0.GenerativeAiEvaluationDefinition
	if err := json.Unmarshal([]byte(evaluationJSON), &evaluation); err != nil {
		return fmt.Errorf("error parsing generative AI evaluation JSON: %w", err)
	}
	dash0.StripGenerativeAiEvaluationServerFields(&evaluation)
	dash0.SetGenerativeAiEvaluationDataset(&evaluation, dataset)
	evaluation.Metadata.Labels.Dash0Comorigin = &origin

	_, err := c.inner.UpdateGenerativeAiEvaluation(ctx, origin, &evaluation, &dataset)
	return err
}

// DeleteEval soft-deletes the generative AI evaluation with
// the given origin. Deleting an already-deleted evaluation returns an error
// that dash0.IsNotFound recognizes.
func (c *dash0Client) DeleteEval(ctx context.Context, origin string, dataset string) error {
	if err := c.inner.DeleteGenerativeAiEvaluation(ctx, origin, &dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Generative AI evaluation deleted with origin: %s", origin))
	return nil
}

// ResolveEval returns the server-assigned id of the
// generative AI evaluation with the given origin, or an empty string when it
// is not in the list. An error from the list call itself is propagated.
func (c *dash0Client) ResolveEval(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListGenerativeAiEvaluations(ctx, &dataset)
	if err != nil {
		return "", err
	}

	id := matchOriginID(items, origin, func(evaluation *dash0.GenerativeAiEvaluationDefinition) (string, *string) {
		if evaluation.Metadata.Labels == nil {
			return "", nil
		}
		return dash0.GetGenerativeAiEvaluationID(evaluation), evaluation.Metadata.Labels.Dash0Comorigin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Generative AI evaluation with origin %q not found in dataset %q; id will be empty", origin, dataset))
		return "", nil
	}

	return id, nil
}
