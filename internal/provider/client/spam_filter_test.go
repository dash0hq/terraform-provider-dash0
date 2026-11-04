package client

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/dash0-api-client-go/dash0test"
)

const minimalV1Alpha1SpamFilterJSON = `{
	"apiVersion": "v1alpha1",
	"kind": "Dash0SpamFilter",
	"metadata": {"name": "repro-filter"},
	"spec": {
		"filter": [{"key": "body", "value": "spam"}],
		"contexts": ["log"]
	}
}`

// trackingSpamFilterFake returns a dash0test.MockClient whose
// UpdateSpamFilterFunc and DeleteSpamFilterFunc record how many calls are
// concurrently "in flight" (i.e. actually executing) at once, tracking the
// high-water mark in maxInFlight. A short sleep on entry widens the window
// during which a real race would show up as more than one call in flight at
// the same time.
func trackingSpamFilterFake(attempts, inFlight, maxInFlight *int32) *dash0test.MockClient {
	track := func() {
		atomic.AddInt32(attempts, 1)
		n := atomic.AddInt32(inFlight, 1)
		for {
			max := atomic.LoadInt32(maxInFlight)
			if n <= max || atomic.CompareAndSwapInt32(maxInFlight, max, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(inFlight, -1)
	}
	return &dash0test.MockClient{
		UpdateSpamFilterFunc: func(ctx context.Context, originOrID string, filter *dash0.SpamFilter, dataset *string) (*dash0.SpamFilter, error) {
			track()
			return filter, nil
		},
		DeleteSpamFilterFunc: func(ctx context.Context, originOrID string, dataset *string) error {
			track()
			return nil
		},
	}
}

// TestUpsertSpamFilter_ConcurrentWritesToSameDatasetAreSerialized guards
// against the second bug reported against dash0_spam_filter: when a
// Terraform apply touches more than one spam filter in the same dataset in a
// single run, Terraform's default parallelism issues those Create/Update
// calls concurrently. The real Dash0 API guards each dataset with an
// optimistic-concurrency "dataset version", so concurrent writers race that
// version and all but one come back with a 409 "please retry" that nothing
// in the stack retries — the user has to re-run `terraform apply` once per
// losing resource.
//
// Rather than retrying after the fact, upsertSpamFilter now serializes
// Create/Update/Delete calls per dataset (see lockDataset), so the
// race never reaches the API in the first place. This test fires two
// concurrent updates at the same dataset and asserts that the fake API never
// observes more than one of them in flight at once, and that both succeed.
func TestUpsertSpamFilter_ConcurrentWritesToSameDatasetAreSerialized(t *testing.T) {
	const dataset = "upsert-same-dataset"
	origins := []string{"tf_filter_a", "tf_filter_b"}

	var attempts, inFlight, maxInFlight int32
	c := &dash0Client{inner: trackingSpamFilterFake(&attempts, &inFlight, &maxInFlight)}

	var wg sync.WaitGroup
	errs := make([]error, len(origins))
	var release sync.WaitGroup
	release.Add(1)

	for i, origin := range origins {
		wg.Add(1)
		go func(i int, origin string) {
			defer wg.Done()
			release.Wait() // start both upserts at the same instant
			errs[i] = c.UpdateSpamFilter(context.Background(), origin, minimalV1Alpha1SpamFilterJSON, dataset)
		}(i, origin)
	}
	release.Done()
	wg.Wait()

	require.EqualValues(t, len(origins), attempts, "both writes should have reached the API")
	assert.EqualValues(t, 1, maxInFlight,
		"writes to the same dataset must be serialized so they never overlap in flight, avoiding the dataset-version race entirely")
	for _, err := range errs {
		assert.NoError(t, err, "serialized writes should never surface a dataset-version conflict")
	}
}

// TestSpamFilterWrites_DifferentDatasetsAreNotSerialized checks that the
// per-dataset lock added to fix the concurrent-409 bug is scoped to a single
// dataset, not global: concurrent writes to unrelated datasets must still be
// able to run at the same time.
//
// Note that the lock registry is process-wide (see lockDataset), so every test
// in this package uses dataset names of its own rather than sharing one.
func TestSpamFilterWrites_DifferentDatasetsAreNotSerialized(t *testing.T) {
	var attempts, inFlight, maxInFlight int32
	c := &dash0Client{inner: trackingSpamFilterFake(&attempts, &inFlight, &maxInFlight)}

	var wg sync.WaitGroup
	var release sync.WaitGroup
	release.Add(1)

	datasets := []string{"unrelated-dataset-a", "unrelated-dataset-b"}
	for i, dataset := range datasets {
		wg.Add(1)
		go func(i int, dataset string) {
			defer wg.Done()
			release.Wait()
			assert.NoError(t, c.UpdateSpamFilter(context.Background(), "tf_filter", minimalV1Alpha1SpamFilterJSON, dataset))
		}(i, dataset)
	}
	release.Done()
	wg.Wait()

	require.EqualValues(t, len(datasets), attempts, "both writes should have reached the API")
	assert.EqualValues(t, len(datasets), maxInFlight,
		"writes to different datasets should not be serialized against each other")
}

// TestDeleteSpamFilter_SerializedWithConcurrentUpdateToSameDataset checks
// that Delete is covered by the same per-dataset lock as Create/Update:
// deleting one spam filter while updating another in the same dataset is
// exactly the kind of concurrent write that raced the dataset version before
// this fix.
func TestDeleteSpamFilter_SerializedWithConcurrentUpdateToSameDataset(t *testing.T) {
	const dataset = "delete-with-update-same-dataset"

	var attempts, inFlight, maxInFlight int32
	c := &dash0Client{inner: trackingSpamFilterFake(&attempts, &inFlight, &maxInFlight)}

	var wg sync.WaitGroup
	var release sync.WaitGroup
	release.Add(1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		release.Wait()
		assert.NoError(t, c.UpdateSpamFilter(context.Background(), "tf_filter_a", minimalV1Alpha1SpamFilterJSON, dataset))
	}()
	go func() {
		defer wg.Done()
		release.Wait()
		assert.NoError(t, c.DeleteSpamFilter(context.Background(), "tf_filter_b", dataset))
	}()
	release.Done()
	wg.Wait()

	require.EqualValues(t, 2, attempts, "both the update and the delete should have reached the API")
	assert.EqualValues(t, 1, maxInFlight, "delete and update to the same dataset must be serialized against each other")
}

const v1alpha2SpamFilterJSONWithAnnotations = `{
	"apiVersion": "v1alpha2",
	"kind": "Dash0SpamFilter",
	"metadata": {"name": "repro-filter"%s},
	"spec": {
		"filter": [{"key": "body", "value": "spam"}],
		"context": "log"
	}
}`

// The upsert sends dash0.com/enabled explicitly, so a config that omits it
// switches back on a filter that was disabled outside Terraform, and a config
// that sets it is sent as written.
func TestUpsertSpamFilter_EnabledAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations string
		want        dash0.SpamFilterAnnotationsDash0Comenabled
	}{
		{name: "omitted defaults to true", annotations: "", want: dash0.True},
		{name: "explicit false is kept", annotations: `, "annotations": {"dash0.com/enabled": "false"}`, want: dash0.False},
		{name: "explicit true is kept", annotations: `, "annotations": {"dash0.com/enabled": "true"}`, want: dash0.True},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent *dash0.SpamFilterV1Alpha2
			c := &dash0Client{inner: &dash0test.MockClient{
				UpdateSpamFilterV1Alpha2Func: func(ctx context.Context, originOrID string, filter *dash0.SpamFilterV1Alpha2, dataset *string) (*dash0.SpamFilterV1Alpha2, error) {
					sent = filter
					return filter, nil
				},
			}}

			body := fmt.Sprintf(v1alpha2SpamFilterJSONWithAnnotations, tt.annotations)
			require.NoError(t, c.UpdateSpamFilter(context.Background(), "tf_enabled", body, "enabled-annotation-"+tt.name))

			require.NotNil(t, sent)
			require.NotNil(t, sent.Metadata.Annotations)
			require.NotNil(t, sent.Metadata.Annotations.Dash0Comenabled)
			assert.Equal(t, tt.want, *sent.Metadata.Annotations.Dash0Comenabled)
		})
	}
}

// The read path must keep dash0.com/enabled so drift detection can see a
// filter that was switched off, while still dropping server-managed labels.
func TestGetSpamFilter_KeepsEnabledAnnotation(t *testing.T) {
	disabled := dash0.False
	id := "server-id"
	c := &dash0Client{inner: &dash0test.MockClient{
		GetSpamFilterFunc: func(ctx context.Context, originOrID string, dataset *string) (dash0.SpamFilterObject, error) {
			return &dash0.SpamFilterV1Alpha2{
				ApiVersion: dash0.V1alpha2,
				Kind:       dash0.SpamFilterDefinitionV1Alpha2KindDash0SpamFilter,
				Metadata: dash0.SpamFilterMetadata{
					Name:        "repro-filter",
					Annotations: &dash0.SpamFilterAnnotations{Dash0Comenabled: &disabled},
					Labels:      &dash0.SpamFilterLabels{Dash0Comid: &id},
				},
			}, nil
		},
	}}

	got, err := c.GetSpamFilter(context.Background(), "tf_enabled", "get-keeps-enabled")
	require.NoError(t, err)
	assert.Contains(t, got, `"dash0.com/enabled":"false"`)
	assert.NotContains(t, got, "server-id", "dash0.com/id is server-managed and must still be stripped")
}

const v1alpha1SpamFilterJSONWithAnnotations = `{
	"apiVersion": "v1alpha1",
	"kind": "Dash0SpamFilter",
	"metadata": {"name": "repro-filter"%s},
	"spec": {
		"filter": [{"key": "body", "value": "spam"}],
		"contexts": ["log"]
	}
}`

// The v1alpha1 branch of the upsert applies the same dash0.com/enabled default
// as v1alpha2.
func TestUpsertSpamFilter_EnabledAnnotation_V1Alpha1(t *testing.T) {
	tests := []struct {
		name        string
		annotations string
		want        dash0.SpamFilterAnnotationsDash0Comenabled
	}{
		{name: "omitted defaults to true", annotations: "", want: dash0.True},
		{name: "explicit false is kept", annotations: `, "annotations": {"dash0.com/enabled": "false"}`, want: dash0.False},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent *dash0.SpamFilter
			c := &dash0Client{inner: &dash0test.MockClient{
				UpdateSpamFilterFunc: func(ctx context.Context, originOrID string, filter *dash0.SpamFilter, dataset *string) (*dash0.SpamFilter, error) {
					sent = filter
					return filter, nil
				},
			}}

			body := fmt.Sprintf(v1alpha1SpamFilterJSONWithAnnotations, tt.annotations)
			require.NoError(t, c.UpdateSpamFilter(context.Background(), "tf_enabled", body, "enabled-annotation-v1alpha1-"+tt.name))

			require.NotNil(t, sent)
			require.NotNil(t, sent.Metadata.Annotations)
			require.NotNil(t, sent.Metadata.Annotations.Dash0Comenabled)
			assert.Equal(t, tt.want, *sent.Metadata.Annotations.Dash0Comenabled)
		})
	}
}
