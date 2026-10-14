package testinfra

import (
	"net"
	"net/url"
	"testing"
)

// DocumentWorkerDSN connects the dedicated fixture identity to an existing test database.
func DocumentWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_document_worker", "helivanta_document_worker")}
	q := url.Values{"sslmode": {"disable"}}
	u.RawQuery = q.Encode()
	return u.String()
}
