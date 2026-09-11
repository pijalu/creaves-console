package actions

import "github.com/gobuffalo/pop/v6"

// withTx runs fn inside a database transaction. When tx is already
// transactional (e.g. wrapped by the pop middleware), fn runs directly on it
// — nesting pop's Connection.Transaction would commit the shared outer
// transaction and make the middleware's own commit fail with a spurious
// "transaction has already been committed or rolled back" error.
func withTx(tx *pop.Connection, fn func(t *pop.Connection) error) error {
	if tx.TX != nil {
		return fn(tx)
	}
	return tx.Transaction(fn)
}
