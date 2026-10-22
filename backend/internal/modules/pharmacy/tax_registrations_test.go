package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

const pkOutlet = "0199e000-0000-7000-8000-000000000003"

var ownerPerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage", "pharmacy.stock.read"}

// taxHarness seeds an Indian and a Pakistani outlet of one pharmacy, grants
// the owner editor access to the Indian one, and pins the clock to
// 2026-11-01 12:00 IST.
func taxHarness(t *testing.T) *gin.Engine {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
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
	w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pkOutlet, "editor", `{"code":"TEST-LAHORE","name":"TEST Lahore Outlet","country_code":"PK","timezone":"Asia/Karachi"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, subject := range []string{"user-editor", "user-owner", "user-pharmacist"} {
		grantCatalogue(t, r, subject, "editor")
	}
	return r
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

	w := do(r, "POST", registrationsPath(pkOutlet), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", ""))
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
