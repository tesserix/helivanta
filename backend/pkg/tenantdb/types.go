package tenantdb

// Migration is one ordered, idempotent schema change. Applied by ID once.
type Migration struct {
	ID  string
	SQL string
}

// DB placeholder until Task 4 implements db.go.
type DB struct{}
