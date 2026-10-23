package supply_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const pakistanMigration = "0009_supply"

// insertLegacyParty writes a party straight to the database, as data that
// predates Pakistan's removal would be: a test-only migration run as the
// owner, which lifts forced RLS for its insert exactly as the real migration
// does for its count.
func insertLegacyParty(t *testing.T, db *tenantdb.DB, country string) error {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	code := strings.ToUpper(id[24:])
	return db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_legacy_party_" + id, SQL: fmt.Sprintf(`
		SELECT set_config('app.actor_subject','user-owner',true),set_config('app.tenant_id','%[1]s',true);
		ALTER TABLE supply_parties NO FORCE ROW LEVEL SECURITY;
		ALTER TABLE supply_party_revisions NO FORCE ROW LEVEL SECURITY;
		INSERT INTO supply_parties (tenant_id,id,code,record_kind,legal_name,trading_name,business_kind,country_code,jurisdiction,registration_number,corporate_group,website,default_credit_days,version)
		VALUES ('%[1]s','%[2]s','TEST-%[3]s','counterparty','TEST Legacy Distributor','','distributor','%[4]s','TEST-REGION','TEST-REG-%[3]s','','',30,1);
		INSERT INTO supply_party_revisions (tenant_id,party_id,version,id,snapshot,actor_subject)
		VALUES ('%[1]s','%[2]s',1,'%[5]s','{}','user-owner');
		SET CONSTRAINTS ALL IMMEDIATE;
		ALTER TABLE supply_party_revisions FORCE ROW LEVEL SECURITY;
		ALTER TABLE supply_parties FORCE ROW LEVEL SECURITY;`, testutil.TenantA, id, code, country, uuid.Must(uuid.NewV7()).String())}})
}

// #1287 D2: one tenant's Pakistan party refuses the migration, though the
// owner role cannot see tenant rows under forced RLS.
func TestSupplyMigrationRefusesPakistanRows(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, supply.New())
	require.NoError(t, insertLegacyParty(t, db, "PK"))
	err := db.Migrate(ctx, rest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "#1287: Pakistan rows: supply_parties 1")
	testutil.RequireForcedRLS(t, ctx, db, "supply_parties", "supply_party_tax_registrations", "supply_premises")
}

// After the migration the database itself refuses Pakistan.
func TestSupplyMigrationPassesAndTightens(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, supply.New())
	require.NoError(t, insertLegacyParty(t, db, "IN"))
	require.NoError(t, db.Migrate(ctx, rest))
	testutil.RequireForcedRLS(t, ctx, db, "supply_parties", "supply_party_tax_registrations", "supply_premises")
	require.Error(t, insertLegacyParty(t, db, "PK"))
}

// #1287: the API refuses a Pakistan party and Pakistan's identifier kinds.
func TestSupplyRefusesPakistan(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	pakistan := strings.Replace(partyBody(0, "TEST Pakistan Distributor"), `"country_code":"IN"`, `"country_code":"PK"`, 1)
	w := testutil.Do(r, "PUT", root, "owner", pakistan)
	require.Equal(t, 400, w.Code, w.Body.String())
	ntn := strings.Replace(partyBody(0, "TEST NTN Distributor"), `"tax_registrations":[]`,
		fmt.Sprintf(`"tax_registrations":[{"id":%q,"kind":"ntn","number":"TEST-TAX-001","jurisdiction":"TEST-MH"}]`, uuid.Must(uuid.NewV7()).String()), 1)
	gstin := strings.Replace(ntn, `"kind":"ntn"`, `"kind":"gstin"`, 1)
	w = testutil.Do(r, "PUT", root, "owner", ntn)
	require.Equal(t, 400, w.Code, w.Body.String())
	// The same body with an Indian identifier is accepted: only the kind differs.
	w = testutil.Do(r, "PUT", root, "owner", gstin)
	require.Equal(t, 200, w.Code, w.Body.String())
}
