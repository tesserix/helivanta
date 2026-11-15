package pharmacy_test

import (
	"encoding/json"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1715: a tax setup command carries its client's UUIDv7, so a resend is
// recognised instead of being recorded a second time.

// secondOutlet is another Indian outlet of the same pharmacy.
const secondOutlet = "0199e000-0000-7000-8000-000000000004"

func taxCommandPath(base string) string { return base + "/" + uuid.Must(uuid.NewV7()).String() }

func TestTaxRegistrationResendReplays(t *testing.T) {
	for name, reason := range map[string]string{"first entry": "", "with a reason": "TEST reason"} {
		t.Run(name, func(t *testing.T) {
			r := taxHarness(t)
			path := taxCommandPath(registrationsPath(pharmaOutlet))
			body := registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", reason)
			first := do(r, "PUT", path, "owner", body)
			require.Equal(t, 201, first.Code, first.Body.String())
			second := do(r, "PUT", path, "owner", body)
			require.Equal(t, 200, second.Code, second.Body.String())
			require.Equal(t, rowID(t, first), rowID(t, second))
			require.Len(t, listRegistrations(t, r, "owner"), 1)
		})
	}
}

func TestTaxRegistrationReplayAfterItsDate(t *testing.T) {
	r := taxHarness(t)
	path := taxCommandPath(registrationsPath(pharmaOutlet))
	body := registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")
	require.Equal(t, 201, do(r, "PUT", path, "owner", body).Code)
	// Two days later the date is in the past; the command already happened.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 3, 6, 30, 0, 0, time.UTC) })
	w := do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, listRegistrations(t, r, "owner"), 1)
}

func TestTaxRegistrationIDReuseConflicts(t *testing.T) {
	body := registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")
	for name, tc := range map[string]struct{ path, token, body string }{
		"different details": {"", "owner", registrationBody("27", "27PQRST5678K2Z1", "2026-11-01", "TEST moved")},
		"different person":  {"", "editor", body},
		"different outlet":  {secondOutlet, "owner", body},
	} {
		t.Run(name, func(t *testing.T) {
			r := taxHarness(t)
			w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+secondOutlet, "editor",
				`{"code":"TEST-SECOND","name":"TEST Second Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			id := uuid.Must(uuid.NewV7()).String()
			require.Equal(t, 201, do(r, "PUT", registrationsPath(pharmaOutlet)+"/"+id, "owner", body).Code)
			outlet := pharmaOutlet
			if tc.path != "" {
				outlet = tc.path
			}
			w = do(r, "PUT", registrationsPath(outlet)+"/"+id, tc.token, tc.body)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "already used")
			require.Len(t, listRegistrations(t, r, "owner"), 1)
			w = do(r, "GET", registrationsPath(secondOutlet), "owner", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			var other struct{ Data []registrationRow }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &other))
			require.Empty(t, other.Data, "nothing is recorded in the other outlet")
		})
	}
}

func TestTaxRegistrationConcurrentResend(t *testing.T) {
	r, db := taxHarnessDB(t)
	path := taxCommandPath(registrationsPath(pharmaOutlet))
	body := registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")
	// Hold the insert trigger's outlet lock so both sends are in flight at
	// once: with the command lock, one waits on the trigger and the other on
	// the command; without it, both pass the lookup and the second insert
	// collides.
	held, release, holder := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		holder <- catalogueTx(t, db, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('pharmacy_outlet_tax_registrations'||?::text||?::text,0))`, testutil.TenantA, pharmaOutlet).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- do(r, "PUT", path, "owner", body).Code }()
	}
	require.Eventually(t, func() bool {
		var waiting int64
		_ = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiting).Error
		})
		return waiting == 2
	}, 10*time.Second, 20*time.Millisecond, "both sends wait on a lock")
	close(release)
	require.NoError(t, <-holder)
	got := []int{<-codes, <-codes}
	sort.Ints(got)
	require.Equal(t, []int{200, 201}, got)
	require.Len(t, listRegistrations(t, r, "owner"), 1)
}

func TestTaxRegistrationStoresItsIntent(t *testing.T) {
	r, db := taxHarnessDB(t)
	require.Equal(t, 201, do(r, "PUT", taxCommandPath(registrationsPath(pharmaOutlet)), "owner", registrationBody("07", "07ABCDE1234F1Z2", "2026-11-01", "")).Code)
	var missing int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pharmacy_outlet_tax_registrations WHERE intent_sha256 IS NULL`).Scan(&missing).Error
	}))
	require.Zero(t, missing)
}

func rowID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var row struct{ ID string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	require.NotEmpty(t, row.ID)
	return row.ID
}

func classificationCount(t *testing.T, r *gin.Engine, product string) int {
	t.Helper()
	w := do(r, "GET", classificationsPath(product), "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct{ Data []json.RawMessage }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	return len(page.Data)
}

func TestTaxClassificationResendReplays(t *testing.T) {
	for name, reason := range map[string]string{"first entry": "", "with a reason": "TEST reason"} {
		t.Run(name, func(t *testing.T) {
			r := taxHarness(t)
			product := putTaxProduct(t, r, "TEST-RESEND", "active")
			path := taxCommandPath(classificationsPath(product))
			body := classificationBody("30041010", "taxable", "5", "", "2026-11-01", reason)
			first := do(r, "PUT", path, "owner", body)
			require.Equal(t, 201, first.Code, first.Body.String())
			second := do(r, "PUT", path, "owner", body)
			require.Equal(t, 200, second.Code, second.Body.String())
			require.Equal(t, rowID(t, first), rowID(t, second))
			require.Equal(t, 1, classificationCount(t, r, product))
		})
	}
}

func TestTaxClassificationReplayAfterItsDate(t *testing.T) {
	r := taxHarness(t)
	product := putTaxProduct(t, r, "TEST-LATER", "active")
	path := taxCommandPath(classificationsPath(product))
	body := classificationBody("30041010", "taxable", "5", "", "2026-11-01", "")
	require.Equal(t, 201, do(r, "PUT", path, "owner", body).Code)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 3, 6, 30, 0, 0, time.UTC) })
	w := do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, classificationCount(t, r, product))
}

func TestTaxClassificationIDReuseConflicts(t *testing.T) {
	body := classificationBody("30041010", "taxable", "5", "", "2026-11-01", "")
	for name, tc := range map[string]struct {
		token, body  string
		otherProduct bool
	}{
		"different rate":   {"owner", classificationBody("30041010", "taxable", "12", "", "2026-11-01", "TEST rate"), false},
		"different person": {"editor", body, false},
		"another product":  {"owner", body, true},
	} {
		t.Run(name, func(t *testing.T) {
			r := taxHarness(t)
			product := putTaxProduct(t, r, "TEST-FIRST", "active")
			other := putTaxProduct(t, r, "TEST-OTHER", "active")
			id := uuid.Must(uuid.NewV7()).String()
			require.Equal(t, 201, do(r, "PUT", classificationsPath(product)+"/"+id, "owner", body).Code)
			target := product
			if tc.otherProduct {
				target = other
			}
			w := do(r, "PUT", classificationsPath(target)+"/"+id, tc.token, tc.body)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "already used")
			require.Equal(t, 1, classificationCount(t, r, product))
			require.Zero(t, classificationCount(t, r, other))
		})
	}
}

func TestTaxClassificationRefusesABadCommandID(t *testing.T) {
	r := taxHarness(t)
	product := putTaxProduct(t, r, "TEST-BADID", "active")
	// A UUIDv4 is well formed but not a command id; like every pharmacy
	// path id that is not a UUIDv7, it is not found.
	w := do(r, "PUT", classificationsPath(product)+"/"+uuid.New().String(), "owner", classificationBody("30041010", "taxable", "5", "", "2026-11-01", ""))
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Zero(t, classificationCount(t, r, product))
}
