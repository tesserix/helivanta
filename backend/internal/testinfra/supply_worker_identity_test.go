package testinfra

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestSupplyEvidenceProcessorRefusesInheritedRolesOrMemberships(t *testing.T) {
	app, _, _ := StartPostgres(t)
	parsed, err := url.Parse(app)
	require.NoError(t, err)
	db, err := tenantdb.OpenRuntime(t.Context(), SupplyEvidenceWorkerDSN(t, parsed.Path[1:]))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runSQL(ctx, "hms", `REVOKE hms_app FROM helivanta_supply_evidence_worker; ALTER ROLE helivanta_supply_evidence_worker NOINHERIT;`))
	})
	cloud := NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	scanner, err := attachments.NewClamAV(ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	_, err = supply.NewEvidenceProcessor(t.Context(), db, files, scanner)
	require.NoError(t, err)
	for _, sql := range []string{
		`ALTER ROLE helivanta_supply_evidence_worker INHERIT;`,
		`ALTER ROLE helivanta_supply_evidence_worker NOINHERIT; GRANT hms_app TO helivanta_supply_evidence_worker;`,
	} {
		require.NoError(t, runSQL(t.Context(), "hms", sql))
		_, err = supply.NewEvidenceProcessor(t.Context(), db, files, scanner)
		require.Error(t, err, "processing credentials must not inherit any other domain's privileges")
	}
}
