package tenantdb

// Migration is one ordered, idempotent schema change. Applied by ID once.
type Migration struct {
	ID                string
	SQL               string
	ConcurrentIndexes []ConcurrentIndex
}

// ConcurrentIndex is a non-unique public-schema B-tree built without blocking writers.
type ConcurrentIndex struct {
	Name    string        `json:"name"`
	Table   string        `json:"table"`
	Columns []IndexColumn `json:"columns"`
}

// IndexColumn preserves a simple key's order and PostgreSQL's default null ordering.
type IndexColumn struct {
	Name       string `json:"name"`
	Descending bool   `json:"descending"`
}
