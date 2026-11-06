package workforce_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"                   //nolint:depguard // published contract under test
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract" //nolint:depguard // published contract under test
	"github.com/tesserix/helivanta/internal/modules/workforce"                                  //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const establishmentKindMigration = "0088_workforce_establishment_kind"

type establishmentHarness struct {
	router   *gin.Engine
	db       *tenantdb.DB
	bus      *events.Bus
	module   platform.Module
	hospital string
}

func newEstablishmentRouter(t *testing.T, module platform.Module) *establishmentHarness {
	t.Helper()
	router, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{module},
	})
	return &establishmentHarness{router: router, db: db, bus: bus, module: module, hospital: uuid.Must(uuid.NewV7()).String()}
}

func newEstablishmentHarness(t *testing.T, module platform.Module) *establishmentHarness {
	t.Helper()
	h := newEstablishmentRouter(t, module)
	router := h.router
	response := testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"Synthetic Establishment Hospital"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/access/user-admin", "admin", `{"level":"manager"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	return h
}

func (h *establishmentHarness) tx(t *testing.T, establishment string, fn func(*gorm.DB) error) error {
	t.Helper()
	return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_leave_routing','authorized',true)`, establishment).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func (h *establishmentHarness) pharmacy(t *testing.T, ownership string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.tx(t, id, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Employer Pharmacy','pharmacy',?)`, testutil.TenantA, id, ownership).Error
	}))
	return id
}

type withoutEstablishmentKind struct{ platform.Module }

func (m withoutEstablishmentKind) Migrations() []tenantdb.Migration {
	var kept []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID != establishmentKindMigration {
			kept = append(kept, migration)
		}
	}
	return kept
}

func TestEstablishmentKindMigrationReadsExistingHospitalsAsHospitals(t *testing.T) {
	// Seeded in SQL because the served routes read establishment_kind and never run before 0088 applies.
	h := newEstablishmentRouter(t, withoutEstablishmentKind{Module: workforce.New()})
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name) VALUES(?,?,'Synthetic Establishment Hospital')`, testutil.TenantA, h.hospital).Error
	}))
	var migration []tenantdb.Migration
	for _, candidate := range workforce.New().Migrations() {
		if candidate.ID == establishmentKindMigration {
			migration = append(migration, candidate)
		}
	}
	require.Len(t, migration, 1)
	require.NoError(t, h.db.Migrate(t.Context(), migration))
	var row struct {
		EstablishmentKind     string
		PharmacyOwnershipType *string
	}
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT establishment_kind,pharmacy_ownership_type FROM workforce_hospitals WHERE tenant_id=? AND id=?`, testutil.TenantA, h.hospital).Scan(&row).Error
	}))
	require.Equal(t, "hospital", row.EstablishmentKind)
	require.Nil(t, row.PharmacyOwnershipType)
}

func TestEstablishmentKindRequiresAnEmployerOwnershipExactlyForPharmacies(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	insert := func(kind string, ownership any, name string) error {
		return h.tx(t, h.hospital, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,?,?,?)`, testutil.TenantA, uuid.Must(uuid.NewV7()).String(), name, kind, ownership).Error
		})
	}
	for name, refused := range map[string]struct {
		kind      string
		ownership any
		name      string
	}{
		"pharmacy without ownership":    {"pharmacy", nil, "Synthetic pharmacy"},
		"hospital owned pharmacy":       {"pharmacy", "hospital_owned", "Synthetic pharmacy"},
		"unknown ownership":             {"pharmacy", "franchise", "Synthetic pharmacy"},
		"hospital with ownership":       {"hospital", "independent", "Synthetic hospital"},
		"unknown kind":                  {"clinic", nil, "Synthetic clinic"},
		"hospital name over 150":        {"hospital", nil, strings.Repeat("h", 151)},
		"pharmacy name over 200":        {"pharmacy", "chain", strings.Repeat("p", 201)},
		"blank pharmacy name":           {"pharmacy", "independent", "   "},
		"padded hospital name over 150": {"hospital", nil, " " + strings.Repeat("h", 151) + " "},
	} {
		var refusal *pgconn.PgError
		err := insert(refused.kind, refused.ownership, refused.name)
		require.True(t, errors.As(err, &refusal), "%s: expected a refusal, got %v", name, err)
		require.Equal(t, "23514", refusal.Code, "%s: %s", name, refusal.Message)
	}
	require.NoError(t, insert("pharmacy", "independent", strings.Repeat("p", 200)))
	require.NoError(t, insert("pharmacy", "chain", "Synthetic chain pharmacy"))
	require.NoError(t, insert("hospital", nil, strings.Repeat("h", 150)))
}

func TestEstablishmentIdentityCannotChange(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "independent")
	for name, statement := range map[string]string{
		"hospital becomes pharmacy": `UPDATE workforce_hospitals SET establishment_kind='pharmacy',pharmacy_ownership_type='chain' WHERE id='` + h.hospital + `'`,
		"pharmacy becomes hospital": `UPDATE workforce_hospitals SET establishment_kind='hospital',pharmacy_ownership_type=NULL WHERE id='` + pharmacy + `'`,
		"ownership changes":         `UPDATE workforce_hospitals SET pharmacy_ownership_type='chain' WHERE id='` + pharmacy + `'`,
		"pharmacy is renamed":       `UPDATE workforce_hospitals SET name='Synthetic renamed pharmacy' WHERE id='` + pharmacy + `'`,
		"pharmacy identifier moves": `UPDATE workforce_hospitals SET id='` + uuid.Must(uuid.NewV7()).String() + `' WHERE id='` + pharmacy + `'`,
	} {
		err := h.tx(t, h.hospital, func(tx *gorm.DB) error { return tx.Exec(statement).Error })
		var refusal *pgconn.PgError
		require.True(t, errors.As(err, &refusal), "%s: expected a refusal, got %v", name, err)
		require.Equal(t, "23514", refusal.Code, "%s: %s", name, refusal.Message)
		require.Contains(t, refusal.Message, "workforce establishment identity is immutable", name)
	}
	response := testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"Synthetic Renamed Hospital"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, `{"id":"`+h.hospital+`","name":"Synthetic Renamed Hospital"}`, response.Body.String())
}

func TestEstablishmentTablesRefuseAPharmacyEstablishment(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "chain")
	unregistered := uuid.Must(uuid.NewV7()).String()
	for name, write := range map[string]struct {
		establishment string
		statement     string
		args          []any
	}{
		"staff":                              {pharmacy, `INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-PHARMACY','Synthetic pharmacist','pharmacist','active')`, []any{testutil.TenantA, pharmacy, uuid.Must(uuid.NewV7()).String()}},
		"access grant":                       {pharmacy, `INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-pharmacy','manager')`, []any{testutil.TenantA, pharmacy}},
		"hr master":                          {pharmacy, `INSERT INTO workforce_hr_masters(tenant_id,hospital_id,id,kind,code,name,active,version,revision_id) VALUES(?,?,?,'department','SYN-RX','Synthetic dispensary',true,1,?)`, []any{testutil.TenantA, pharmacy, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()}},
		"leave routing":                      {pharmacy, `INSERT INTO workforce_leave_routing_revisions(tenant_id,hospital_id,version,mode,source_reference,reviewed_by_subject) VALUES(?,?,1,'hospital','Synthetic pharmacy routing','user-admin')`, []any{testutil.TenantA, pharmacy}},
		"leave routing without registry row": {unregistered, `INSERT INTO workforce_leave_routing_revisions(tenant_id,hospital_id,version,mode,source_reference,reviewed_by_subject) VALUES(?,?,1,'hospital','Synthetic missing routing','user-admin')`, []any{testutil.TenantA, unregistered}},
		"access grant moves to pharmacy":     {h.hospital, `UPDATE workforce_hospital_access SET hospital_id=? WHERE tenant_id=? AND hospital_id=?`, []any{pharmacy, testutil.TenantA, h.hospital}},
	} {
		err := h.tx(t, write.establishment, func(tx *gorm.DB) error { return tx.Exec(write.statement, write.args...).Error })
		var refusal *pgconn.PgError
		require.True(t, errors.As(err, &refusal), "%s: expected a refusal, got %v", name, err)
		require.Equal(t, "23503", refusal.Code, "%s: %s", name, refusal.Message)
		require.Equal(t, "workforce_hospital_establishment", refusal.ConstraintName, name)
	}
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-hospital-reader','reader')`, testutil.TenantA, h.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_leave_routing_revisions(tenant_id,hospital_id,version,mode,source_reference,reviewed_by_subject) VALUES(?,?,1,'hospital','Synthetic hospital routing','user-admin')`, testutil.TenantA, h.hospital).Error
	}))
}

func TestEveryEstablishmentTableIsGuardedOrReachedThroughAGuardedTable(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	inventory := func() (missing []string) {
		require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
			missing = unguardedEstablishmentTables(t, tx)
			return nil
		}))
		return missing
	}
	require.Empty(t, inventory())
	require.NoError(t, h.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "9000_workforce_inventory_probe", SQL: `CREATE TABLE workforce_inventory_probe(tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id));
CREATE TABLE workforce_inventory_orphan(tenant_id uuid NOT NULL,hospital_id uuid NOT NULL);
CREATE TABLE workforce_inventory_reached(tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id));
CREATE TABLE workforce_inventory_unmapped(tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_hospital uuid NOT NULL,staff_id uuid NOT NULL,FOREIGN KEY(tenant_id,staff_hospital,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id));`}}))
	require.Equal(t, []string{"workforce_inventory_orphan", "workforce_inventory_probe", "workforce_inventory_unmapped"}, inventory())
}

// unguardedEstablishmentTables lists Workforce tables with a hospital_id that no guard trigger or guarded foreign key covers.
func unguardedEstablishmentTables(t *testing.T, tx *gorm.DB) []string {
	t.Helper()
	// Observation ledgers retain negative answers for unknown or non-hospital references.
	exempt := []string{"workforce_audit", "workforce_organization_observations", "workforce_partner_group_observations"}
	var tables, guarded []string
	require.NoError(t, tx.Raw(`SELECT c.relname FROM pg_class c JOIN pg_namespace s ON s.oid=c.relnamespace AND s.nspname=current_schema()
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='hospital_id' AND NOT a.attisdropped
 WHERE c.relkind IN ('r','p') AND c.relname LIKE 'workforce\_%' ORDER BY 1`).Scan(&tables).Error)
	for _, table := range exempt {
		require.Contains(t, tables, table, "stale exemption")
	}
	require.NoError(t, tx.Raw(`SELECT c.relname FROM pg_trigger g JOIN pg_class c ON c.oid=g.tgrelid JOIN pg_proc p ON p.oid=g.tgfoid
 JOIN pg_namespace s ON s.oid=c.relnamespace AND s.nspname=current_schema()
 WHERE p.proname='workforce_require_hospital_establishment' AND NOT g.tgisinternal AND g.tgenabled IN ('O','A')
  AND g.tgtype & 23 = 23 AND (cardinality(g.tgattr::int2[]) = 0 OR (SELECT array_agg(a.attnum) FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname IN ('tenant_id','hospital_id')) <@ g.tgattr::int2[])`).Scan(&guarded).Error)
	var edges []struct {
		Source, Target string
		Mapped         bool
	}
	require.NoError(t, tx.Raw(`SELECT c.relname AS source, f.relname AS target,
  bool_or(la.attname='hospital_id' AND ra.attname=CASE WHEN f.relname='workforce_hospitals' THEN 'id' ELSE 'hospital_id' END)
  AND bool_or(la.attname='tenant_id' AND ra.attname='tenant_id') AS mapped
 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_class f ON f.oid=k.confrelid
 JOIN pg_namespace s ON s.oid=c.relnamespace AND s.nspname=current_schema()
 CROSS JOIN LATERAL unnest(k.conkey,k.confkey) AS m(l,r)
 JOIN pg_attribute la ON la.attrelid=k.conrelid AND la.attnum=m.l
 JOIN pg_attribute ra ON ra.attrelid=k.confrelid AND ra.attnum=m.r
 WHERE k.contype='f' AND c.relname LIKE 'workforce\_%'
 GROUP BY k.oid,c.relname,f.relname`).Scan(&edges).Error)
	covered := map[string]bool{}
	for _, table := range guarded {
		covered[table] = true
	}
	var missing []string
	for _, edge := range edges {
		if edge.Target == "workforce_hospitals" && !covered[edge.Source] {
			missing = append(missing, edge.Source)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, edge := range edges {
			if edge.Mapped && edge.Target != "workforce_hospitals" && covered[edge.Target] && !covered[edge.Source] {
				covered[edge.Source], changed = true, true
			}
		}
	}
	for _, table := range tables {
		if !covered[table] && !slices.Contains(exempt, table) && !slices.Contains(missing, table) {
			missing = append(missing, table)
		}
	}
	slices.Sort(missing)
	return slices.Compact(missing)
}

func TestOrganizationObservationReportsAPharmacyEstablishmentAsAbsent(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "independent")
	var consumer events.Consumer
	for _, candidate := range h.module.Consumers(platform.Deps{Bus: h.bus}) {
		if candidate.Name == "workforce-organization-reference" {
			consumer = candidate
		}
	}
	require.NotNil(t, consumer.Handle)
	observe := func(establishment string) bool {
		request := uuid.Must(uuid.NewV7())
		raw, err := json.Marshal(organizationcontract.HospitalReferenceRequestedData{RequestID: request, HospitalID: uuid.MustParse(establishment)})
		require.NoError(t, err)
		event := events.Event{Type: organizationcontract.SubjectHospitalReferenceRequested, Version: 1, TenantID: testutil.TenantA, Data: raw}
		require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, event) }))
		var exists []bool
		require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT observed_exists FROM workforce_organization_observations WHERE tenant_id=? AND request_id=?`, testutil.TenantA, request).Scan(&exists).Error
		}))
		require.Len(t, exists, 1)
		return exists[0]
	}
	require.True(t, observe(h.hospital))
	require.False(t, observe(pharmacy))
}

func TestPartnerGroupObservationReportsOnlyHospitalsAsExisting(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "independent")
	var consumer events.Consumer
	for _, candidate := range h.module.Consumers(platform.Deps{Bus: h.bus}) {
		if candidate.Name == "workforce-partner-group-hospital-scope" {
			consumer = candidate
		}
	}
	require.NotNil(t, consumer.Handle)
	for _, candidate := range []struct {
		name, id string
		exists   bool
	}{
		{"hospital", h.hospital, true}, {"independent pharmacy", pharmacy, false}, {"unknown", uuid.Must(uuid.NewV7()).String(), false},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			group := uuid.Must(uuid.NewV7())
			raw, err := json.Marshal(iamcontract.PartnerGroupHospitalScopeRequestedData{GroupID: group, HospitalID: uuid.MustParse(candidate.id)})
			require.NoError(t, err)
			event := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: iamcontract.SubjectPartnerGroupHospitalScopeRequested, Version: 1, TenantID: testutil.TenantA, Data: raw}
			for range 2 {
				require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, event) }))
			}
			var observed []bool
			require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT observed_exists FROM workforce_partner_group_observations WHERE tenant_id=? AND group_id=? AND hospital_id=?`, testutil.TenantA, group, candidate.id).Scan(&observed).Error
			}))
			require.Equal(t, []bool{candidate.exists}, observed, "duplicates preserve one exact hospital-kind observation")
		})
	}
}
