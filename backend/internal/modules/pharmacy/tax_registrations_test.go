package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// schemelessOutlet is a legacy outlet whose country has no profile, and so
// no tax scheme (#1287): only data that predates a country's removal can be
// one, so it is written straight to the database.
const schemelessOutlet = "0199e000-0000-7000-8000-000000000003"

var ownerPerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage", "pharmacy.stock.read"}

// taxHarness seeds an Indian and a schemeless outlet of one pharmacy, grants
// the owner editor access to the Indian one, and pins the clock to
// 2026-11-01 12:00 IST.
func taxHarness(t *testing.T) *gin.Engine {
	t.Helper()
	r, _ := taxHarnessDB(t)
	return r
}

func taxHarnessDB(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "owner": testutil.TenantA, "pharmacist": testutil.TenantA, "ungranted": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     ownerPerms,
			"owner":      ownerPerms,
			"pharmacist": {"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.stock.read"},
			"ungranted":  ownerPerms,
			"foreign":    ownerPerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	seedSchemelessOutlet(t, db)
	for _, subject := range []string{"user-editor", "user-owner", "user-pharmacist"} {
		grantCatalogue(t, r, subject, "editor")
	}
	return r, db
}

func registrationsPath(outlet string) string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + outlet + "/tax-registrations"
}

func registrationBody(jurisdiction, identifier, effective, reason string) string {
	if reason == "" {
		return fmt.Sprintf(`{"jurisdiction_code":%q,"identifier":%q,"effective_from":%q}`, jurisdiction, identifier, effective)
	}
	return fmt.Sprintf(`{"jurisdiction_code":%q,"identifier":%q,"effective_from":%q,"reason":%q}`, jurisdiction, identifier, effective, reason)
}

type registrationRow struct {
	ID               string `json:"id"`
	JurisdictionCode string `json:"jurisdiction_code"`
	Identifier       string `json:"identifier"`
	EffectiveFrom    string `json:"effective_from"`
	Reason           string `json:"reason"`
	RecordedBy       string `json:"recorded_by"`
	InForce          bool   `json:"in_force"`
	Replaced         bool   `json:"replaced"`
}

func listRegistrations(t *testing.T, r *gin.Engine, token string) []registrationRow {
	t.Helper()
	w := do(r, "GET", registrationsPath(pharmaOutlet), token, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct{ Data []registrationRow }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	return page.Data
}

func TestTaxRegistrationRecordsAndCorrects(t *testing.T) {
	r := taxHarness(t)
	w := do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("07", " 07abcde1234f1z2 ", "2026-11-01", ""))
	require.Equal(t, 201, w.Code, w.Body.String())
	var created registrationRow
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.Equal(t, "07ABCDE1234F1Z2", created.Identifier)
	require.Equal(t, "user-owner", created.RecordedBy)
	require.True(t, created.InForce)

	// A same-day correction needs a reason, then replaces the first row.
	w = do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("27", "27PQRST5678K2Z1", "2026-11-01", ""))
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "reason")
	w = do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("27", "27PQRST5678K2Z1", "2026-11-01", "Registered in Maharashtra"))
	require.Equal(t, 201, w.Code, w.Body.String())

	// A future change is recorded but not yet in force.
	w = do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("04", "04LMNOP4321Q1ZC", "2026-12-01", ""))
	require.Equal(t, 201, w.Code, w.Body.String())

	rows := listRegistrations(t, r, "owner")
	require.Len(t, rows, 3)
	byID := map[string]registrationRow{}
	for _, row := range rows {
		byID[row.Identifier] = row
	}
	require.True(t, byID["07ABCDE1234F1Z2"].Replaced)
	require.False(t, byID["07ABCDE1234F1Z2"].InForce)
	require.True(t, byID["27PQRST5678K2Z1"].InForce)
	require.Equal(t, "Registered in Maharashtra", byID["27PQRST5678K2Z1"].Reason)
	require.False(t, byID["04LMNOP4321Q1ZC"].InForce)
	require.False(t, byID["04LMNOP4321Q1ZC"].Replaced)

	// An owner without an outlet access row still reads the registry.
	require.Len(t, listRegistrations(t, r, "ungranted"), 3)
}

func TestTaxRegistrationRejects(t *testing.T) {
	r := taxHarness(t)
	for name, tc := range map[string]struct {
		body    string
		message string
	}{
		"bad check character":        {registrationBody("07", "07ABCDE1234F1Z3", "2026-11-01", ""), "check character"},
		"bad shape":                  {registrationBody("07", "07ABCDE1234F1Y2", "2026-11-01", ""), "format"},
		"other state's GSTIN":        {registrationBody("27", "07ABCDE1234F1Z2", "2026-11-01", ""), "jurisdiction"},
		"retired state code":         {registrationBody("25", "25ABCDE1234F1Z4", "2026-11-01", ""), "jurisdiction"},
		"before today":               {registrationBody("07", "07ABCDE1234F1Z2", "2026-10-31", ""), "today"},
		"not a date":                 {registrationBody("07", "07ABCDE1234F1Z2", "2026-02-30", ""), ""},
		"unknown field":              {`{"jurisdiction_code":"07","identifier":"07ABCDE1234F1Z2","effective_from":"2026-11-01","kind":"pan"}`, ""},
		"blank reason":               {registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", " "), ""},
		"jurisdiction not two digit": {registrationBody("7", "07ABCDE1234F1Z2", "2026-11-01", ""), ""},
	} {
		t.Run(name, func(t *testing.T) {
			w := do(r, "POST", registrationsPath(pharmaOutlet), "owner", tc.body)
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.message)
		})
	}
	require.Empty(t, listRegistrations(t, r, "owner"))

	w := do(r, "POST", registrationsPath(schemelessOutlet), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", ""))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "tax_scheme_unavailable")

	body := registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")
	require.Equal(t, 403, do(r, "POST", registrationsPath(pharmaOutlet), "pharmacist", body).Code)
	require.Equal(t, 403, do(r, "GET", registrationsPath(pharmaOutlet), "pharmacist", "").Code)
	require.Equal(t, 404, do(r, "POST", registrationsPath(pharmaOutlet), "foreign", body).Code)
	require.Equal(t, 404, do(r, "GET", registrationsPath(pharmaOutlet), "foreign", "").Code)
	require.Equal(t, 404, do(r, "POST", registrationsPath("0199e000-0000-7000-8000-000000000099"), "owner", body).Code)
}

// At 00:30 in India it is still the previous day in UTC; "today" is the
// outlet's date.
func TestTaxRegistrationUsesTheOutletsLocalDate(t *testing.T) {
	r := taxHarness(t)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 19, 0, 0, 0, time.UTC) })
	w := do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", ""))
	require.Equal(t, 400, w.Code, w.Body.String())
	w = do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-02", ""))
	require.Equal(t, 201, w.Code, w.Body.String())
	rows := listRegistrations(t, r, "owner")
	require.Len(t, rows, 1)
	require.True(t, rows[0].InForce)
}

// Two rows for one date recorded in one transaction share created_at, so
// only seq (the order of recording) can say which one is in force.
func TestTaxSameTransactionCorrectionIsDecidedBySeq(t *testing.T) {
	r, db := taxHarnessDB(t)
	product := putTaxProduct(t, r, "TEST-SEQ", "active")
	// IDs are handed out newest first, so neither created_at nor id can
	// stand in for the order of recording.
	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[len(ids)-1-i] = uuid.Must(uuid.NewV7())
	}
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		for i := range 6 {
			reason := ""
			if i > 0 {
				reason = fmt.Sprintf("Correction %d", i)
			}
			if err := tx.Exec(`INSERT INTO pharmacy_outlet_tax_registrations
 (id,tenant_id,outlet_id,jurisdiction_code,identifier_kind,identifier,effective_from,reason,recorded_by)
 VALUES (?,?,?,'07','gstin','07ABCDE1234F1Z2','2026-11-01',NULLIF(?,''),'user-owner')`,
				ids[i], testutil.TenantA, pharmaOutlet, reason).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO pharmacy_product_tax_classifications
 (id,tenant_id,outlet_id,product_id,classification_code,supply_nature,rate_ppm,effective_from,reason,recorded_by)
 VALUES (?,?,?,?,'3004','taxable',?,'2026-11-01',NULLIF(?,''),'user-editor')`,
				ids[i], testutil.TenantA, pharmaOutlet, product, int64(10000*(i+1)), reason).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	for _, row := range listRegistrations(t, r, "owner") {
		require.Equal(t, row.Reason == "Correction 5", row.InForce, row.Reason)
		require.Equal(t, row.Reason != "Correction 5", row.Replaced, row.Reason)
	}
	require.Equal(t, int64(60000), getProductTax(t, r, product).RatePPM)
	w := do(r, "GET", classificationsPath(product), "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct {
			RatePPM int64 `json:"rate_ppm"`
			InForce bool  `json:"in_force"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 6)
	for _, row := range page.Data {
		require.Equal(t, row.RatePPM == 60000, row.InForce, row.RatePPM)
	}
}

// The row recorded last must win even when its insert started first: seq
// is assigned after the per-scope lock, not when the row is formed.
func TestTaxCorrectionRecordedLastWinsUnderContention(t *testing.T) {
	r, db := taxHarnessDB(t)
	insert := func(tx *gorm.DB, identifier, reason string) error {
		return tx.Exec(`INSERT INTO pharmacy_outlet_tax_registrations
 (id,tenant_id,outlet_id,jurisdiction_code,identifier_kind,identifier,effective_from,reason,recorded_by)
 VALUES (?,?,?,'07','gstin',?,'2026-11-01',NULLIF(?,''),'user-owner')`,
			uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, identifier, reason).Error
	}
	held := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- catalogueTx(t, db, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('pharmacy_outlet_tax_registrations'||?::text||?::text,0))`, testutil.TenantA, pharmaOutlet).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return insert(tx, "07ABCDE1234F1Z2", "")
		})
	}()
	<-held
	second := make(chan error, 1)
	go func() {
		second <- catalogueTx(t, db, func(tx *gorm.DB) error { return insert(tx, "07PQRST5678K1Z5", "Recorded last") })
	}()
	// Wait until the second insert is queued behind the lock.
	require.Eventually(t, func() bool {
		var waiting int64
		_ = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiting).Error
		})
		return waiting > 0
	}, 10*time.Second, 20*time.Millisecond)
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	for _, row := range listRegistrations(t, r, "owner") {
		require.Equal(t, row.Reason == "Recorded last", row.InForce, row.Identifier)
	}
}

// The registration in force comes from the server, not from scanning a
// history page that may not hold it.
func TestTaxRegistrationInForceRoute(t *testing.T) {
	r := taxHarness(t)
	inForce := func() map[string]any {
		w := do(r, "GET", registrationsPath(pharmaOutlet)+"/in-force", "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var body struct {
			Registration map[string]any `json:"registration"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Registration
	}
	require.Nil(t, inForce())
	require.Equal(t, 201, do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")).Code)
	for i := range 3 {
		require.Equal(t, 201, do(r, "POST", registrationsPath(pharmaOutlet), "owner", registrationBody("04", "04LMNOP4321Q1ZC", fmt.Sprintf("2027-0%d-01", i+1), "")).Code)
	}
	got := inForce()
	require.Equal(t, "07ABCDE1234F1Z2", got["identifier"])
	require.Equal(t, true, got["in_force"])
	require.Equal(t, 403, do(r, "GET", registrationsPath(pharmaOutlet)+"/in-force", "pharmacist", "").Code)
	require.Equal(t, 404, do(r, "GET", registrationsPath(pharmaOutlet)+"/in-force", "foreign", "").Code)
	// The outlet must belong to the pharmacy in the path.
	other := "0199e000-0000-7000-8000-000000000020"
	w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+other, "editor", `{"code":"TEST-OTHER","name":"TEST Other Pharmacy","ownership_type":"independent"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	crossed := strings.Replace(registrationsPath(pharmaOutlet), pharmaOrganization, other, 1)
	require.Equal(t, 404, do(r, "GET", crossed+"/in-force", "owner", "").Code)
	require.Equal(t, 404, do(r, "GET", crossed, "owner", "").Code)
}

func seedSchemelessOutlet(t *testing.T, db *tenantdb.DB) {
	t.Helper()
	legacyTx(t, db, func(tx *gorm.DB) error { return seedLegacyOutlet(tx, schemelessOutlet, "XX", "UTC") })
}
