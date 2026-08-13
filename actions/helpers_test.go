package actions

// Pure unit tests for the dependency-free helper functions in the actions
// package (M7 of TESTING_PLAN.md).
//
// Scope note: consolidation_runner.go, dashboard.go, users.go and
// event_processor.go are dominated by code that requires a live
// buffalo.Context and/or a *pop.Connection (DB). Those paths are covered by
// the sqlite-tagged integration tests (event_processor_test.go etc.). The
// functions exercised here are the only ones that perform no DB access and
// take no buffalo.Context, so they run under a bare `go test ./actions/`
// with no build tags.

import (
	"testing"

	"github.com/gobuffalo/pop/v6"
	"github.com/stretchr/testify/assert"
)

// sentinelTx returns a unique, non-nil *pop.Connection used only as an opaque
// identity token. It is never opened and no methods are invoked on it, so no
// database or dialect driver is required.
func sentinelTx() *pop.Connection {
	return &pop.Connection{}
}

func TestNewEventProcessor_WiresTransaction(t *testing.T) {
	tx := sentinelTx()

	ep := NewEventProcessor(tx)

	assert.NotNil(t, ep)
	// The processor must retain the exact connection it was given so that all
	// of its queries participate in the caller's transaction.
	assert.Same(t, tx, ep.tx)
}

func TestNewConsolidationRunner_WiresTransactionAndProcessor(t *testing.T) {
	tx := sentinelTx()

	cr := NewConsolidationRunner(tx)

	assert.NotNil(t, cr)
	// The runner keeps the connection...
	assert.Same(t, tx, cr.tx)
	// ...and builds an EventProcessor wired with the *same* connection, so the
	// runner and its processor never operate on different transactions.
	assert.NotNil(t, cr.processor)
	assert.Same(t, tx, cr.processor.tx)
}

func TestNewConsolidationRunner_DistinctConnectionsPerInstance(t *testing.T) {
	// Each runner must capture its own connection rather than sharing global
	// state; two runners built from different connections stay independent.
	tx1 := sentinelTx()
	tx2 := sentinelTx()

	cr1 := NewConsolidationRunner(tx1)
	cr2 := NewConsolidationRunner(tx2)

	assert.Same(t, tx1, cr1.tx)
	assert.Same(t, tx2, cr2.tx)
	assert.Same(t, tx1, cr1.processor.tx)
	assert.Same(t, tx2, cr2.processor.tx)
	assert.NotSame(t, cr1.processor, cr2.processor)
}

func TestConsolidationRunner_RunDryRun_NotImplemented(t *testing.T) {
	// RunDryRun does not touch the connection, so a zero-value runner is
	// sufficient. It must surface an explicit "not implemented" error rather
	// than silently succeeding or panicking.
	cr := &ConsolidationRunner{}

	result, err := cr.RunDryRun()

	assert.Nil(t, result, "dry-run must not fabricate a result")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
	assert.Contains(t, err.Error(), "dry-run")
}
