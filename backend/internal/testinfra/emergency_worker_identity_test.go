package testinfra

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestEmergencyProcessorRefusesInheritedRolesOrMemberships(t *testing.T) {
	app, _, _ := StartPostgres(t)
	parsed, err := url.Parse(app)
	require.NoError(t, err)
	db, err := tenantdb.OpenRuntime(t.Context(), EmergencyWorkerDSN(t, parsed.Path[1:]))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runSQL(ctx, "hms", `REVOKE hms_app FROM helivanta_emergency_worker; ALTER ROLE helivanta_emergency_worker NOINHERIT;`))
	})
	_, err = emergency.NewDispatchProcessor(t.Context(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		`ALTER ROLE helivanta_emergency_worker INHERIT;`,
		`ALTER ROLE helivanta_emergency_worker NOINHERIT; GRANT hms_app TO helivanta_emergency_worker;`,
	} {
		require.NoError(t, runSQL(t.Context(), "hms", statement))
		_, err = emergency.NewDispatchProcessor(t.Context(), db)
		require.Error(t, err, "NOINHERIT alone must not permit SET ROLE into another domain")
	}
}
