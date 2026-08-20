package client

import "sync"

// datasetLocks holds one *sync.Mutex per dataset name, serializing writes that
// contend for the same dataset's version. It is deliberately package-level
// rather than a field on dash0Client: Configure builds a fresh dash0Client for
// every provider instance (see NewDash0Client), so two aliased provider blocks
// pointing at the same dataset would otherwise take different mutexes and race
// each other exactly as unaliased resources did before this lock existed.
//
// The map grows by one entry per distinct dataset name written during a run.
// A provider process is scoped to a single plan or apply over a bounded set of
// datasets, so there is nothing to evict.
var datasetLocks sync.Map

// lockDataset acquires the write lock for the given dataset and returns a
// function that releases it.
//
// The Dash0 API guards each dataset with an optimistic-concurrency "dataset
// version". Asset kinds that share one document per dataset — spam filters
// today — bump that version on every write, so concurrent writers race it and
// all but one come back with a 409 that nothing in the stack retries: neither
// the api-client-go transport (which retries only 429 and 5xx) nor the
// provider. Terraform's default parallelism of 10 makes that race the norm
// rather than the exception whenever a single apply touches more than one such
// asset in the same dataset. Serializing the writes prevents the race instead
// of reacting to it after the fact.
//
// The key is the dataset name alone, which can over-serialize: two provider
// blocks authenticated against different Dash0 organizations that both write a
// dataset of the same name (`default` exists in every organization) share a
// mutex even though they contend for different dataset versions. That is a
// deliberate trade: dash0Client carries no organization identity to key on, the
// affected write volume is tiny, and the cost of the alternative — deriving a
// key that turns out not to identify a dataset uniquely — is the silent return
// of the very 409 this prevents.
//
// Callers must invoke the returned function, conventionally via defer:
//
//	unlock := lockDataset(dataset)
//	defer unlock()
//
// This lock is in-process only. It cannot serialize against concurrent
// `terraform apply` runs, the Dash0 web app, the CLI, or the Operator writing
// the same dataset; surviving those needs a retry on 409 (dash0.IsConflict) and
// is tracked separately.
func lockDataset(dataset string) func() {
	value, _ := datasetLocks.LoadOrStore(dataset, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
