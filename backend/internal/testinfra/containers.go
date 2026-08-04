// Package testinfra provides test infrastructure helpers.
package testinfra

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPostgres boots postgres:16, creates the non-BYPASSRLS app role,
// and returns (appDSN, adminDSN). Mirrors dev/init-db.sql.
func StartPostgres(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("hms"),
		tcpostgres.WithUsername("hms"),
		tcpostgres.WithPassword("hms"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })

	adminDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	_, _, err = pg.Exec(ctx, []string{"psql", "-U", "hms", "-d", "hms", "-c", `
		CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;
		GRANT USAGE ON SCHEMA public TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;`})
	if err != nil {
		t.Fatalf("create app role: %v", err)
	}
	host, _ := pg.Host(ctx)
	port, _ := pg.MappedPort(ctx, "5432/tcp")
	appDSN := "postgres://hms_app:hms_app@" + host + ":" + port.Port() + "/hms?sslmode=disable"
	return appDSN, adminDSN
}

// StartNATS boots nats:2.10-alpine and returns the connection URL.
func StartNATS(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	nats, err := tcnats.Run(ctx, "nats:2.10-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("4222/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start nats: %v", err)
	}
	t.Cleanup(func() { _ = nats.Terminate(context.Background()) })

	uri, err := nats.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("nats dsn: %v", err)
	}

	return uri
}
