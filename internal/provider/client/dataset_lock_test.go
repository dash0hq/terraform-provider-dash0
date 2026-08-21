package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockDataset_SeparateClientsSameDatasetAreSerialized is the regression
// test for the hole in the first version of this fix, where the lock registry
// was a field on dash0Client. Configure builds a fresh dash0Client per provider
// instance, so two aliased provider blocks pointing at the same account and
// dataset took different mutexes for the same dataset name and raced the
// dataset version anyway — the 409 the lock exists to prevent.
//
// Two distinct clients writing the same dataset must therefore never overlap in
// flight, exactly as two resources on one client must not.
func TestLockDataset_SeparateClientsSameDatasetAreSerialized(t *testing.T) {
	const dataset = "two-clients-same-dataset"

	// One fake shared by both clients, so in-flight counts observe the writes
	// of both — the fake stands in for the single dataset version on the server
	// that both provider aliases contend for.
	var attempts, inFlight, maxInFlight int32
	fake := trackingSpamFilterFake(&attempts, &inFlight, &maxInFlight)
	clientA := &dash0Client{inner: fake}
	clientB := &dash0Client{inner: fake}

	var wg sync.WaitGroup
	var release sync.WaitGroup
	release.Add(1)

	errs := make([]error, 2)
	for i, c := range []*dash0Client{clientA, clientB} {
		wg.Add(1)
		go func(i int, c *dash0Client) {
			defer wg.Done()
			release.Wait() // start both writes at the same instant
			errs[i] = c.UpdateSpamFilter(context.Background(), "tf_filter", minimalV1Alpha1SpamFilterJSON, dataset)
		}(i, c)
	}
	release.Done()
	wg.Wait()

	require.EqualValues(t, 2, attempts, "both clients' writes should have reached the API")
	assert.EqualValues(t, 1, maxInFlight,
		"writes from separate clients to the same dataset must be serialized: two provider aliases contend for one dataset version")
	for _, err := range errs {
		assert.NoError(t, err)
	}
}

// TestLockDataset_SeparateClientsDifferentDatasetsAreNotSerialized keeps the
// registry honest in the other direction. Without it, making the lock a single
// global mutex would satisfy every other test in this package while serializing
// writes that never contended in the first place.
func TestLockDataset_SeparateClientsDifferentDatasetsAreNotSerialized(t *testing.T) {
	var attempts, inFlight, maxInFlight int32
	fake := trackingSpamFilterFake(&attempts, &inFlight, &maxInFlight)
	clients := []*dash0Client{{inner: fake}, {inner: fake}}
	datasets := []string{"two-clients-dataset-a", "two-clients-dataset-b"}

	var wg sync.WaitGroup
	var release sync.WaitGroup
	release.Add(1)

	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			release.Wait()
			assert.NoError(t, clients[i].UpdateSpamFilter(
				context.Background(), "tf_filter", minimalV1Alpha1SpamFilterJSON, datasets[i]))
		}(i)
	}
	release.Done()
	wg.Wait()

	require.EqualValues(t, 2, attempts, "both writes should have reached the API")
	assert.EqualValues(t, 2, maxInFlight,
		"the lock is keyed per dataset, so writes to unrelated datasets must not serialize even across clients")
}

// TestLockDataset_SameDatasetNameYieldsTheSameMutex covers the registry itself,
// independent of any asset kind: repeated calls for one dataset name must hand
// out the same mutex, and the returned function must release it.
func TestLockDataset_SameDatasetNameYieldsTheSameMutex(t *testing.T) {
	const dataset = "lock-registry-identity"

	unlock := lockDataset(dataset)

	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		lockDataset(dataset)()
	}()

	select {
	case <-acquired:
		t.Fatal("a second lockDataset call for the same dataset acquired the mutex while it was held")
	default:
	}

	unlock()

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("the function returned by lockDataset did not release the mutex")
	}
}

// TestLockDataset_DistinctDatasetNamesYieldDistinctMutexes is the counterpart:
// holding one dataset's lock must not block another's.
func TestLockDataset_DistinctDatasetNamesYieldDistinctMutexes(t *testing.T) {
	unlock := lockDataset("lock-registry-distinct-a")
	defer unlock()

	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		lockDataset("lock-registry-distinct-b")()
	}()

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("holding one dataset's lock blocked an unrelated dataset's lock")
	}
}
