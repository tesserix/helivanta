package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const pharmaOrganization = "0199e000-0000-7000-8000-000000000010"
const pharmaOutlet = "0199e000-0000-7000-8000-000000000001"

func catalogueHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	r, db, _, _ := catalogueSetup(t)
	seedPharmacyOutlet(t, r)
	return r, db
}

func catalogueSetup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "reader": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"editor":  {"pharmacy.accounts.read", "pharmacy.accounts.write", "pharmacy.accounts.payment", "pharmacy.accounts.adjust", "pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.correct"},
			"reader":  {"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.stock.read", "pharmacy.stock.receive"},
			"sibling": {"pharmacy.accounts.read", "pharmacy.accounts.write", "pharmacy.accounts.payment", "pharmacy.accounts.adjust", "pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.stock.read", "pharmacy.stock.receive"},
			"foreign": {"pharmacy.accounts.read", "pharmacy.accounts.write", "pharmacy.accounts.payment", "pharmacy.accounts.adjust", "pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.stock.read", "pharmacy.stock.receive"},
		}, Modules: []platform.Module{pharmacy.New()},
	})
	// Exercise PostgreSQL's microsecond precision even on clocks with lower resolution.
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Callback().Create().Before("gorm:create").Register("pharmacy_test:nanosecond_clock", func(write *gorm.DB) {
			write.NowFunc = func() time.Time {
				return time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
			}
		})
	}))
	return r, db, bus, ctx
}

func productBody(version int, name string) string {
	return fmt.Sprintf(`{"expected_version":%d,"sku":"TEST-AMOX-500","brand_name":%q,"generic_name":"TEST amoxicillin","dosage_form":"capsule","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"TEST Marketing","ingredients":[{"name":"TEST amoxicillin","strength":"500","unit":"mg"}],"pack_size":10,"unit_of_issue":"capsule","currency":"INR","mrp_minor":12500,"status":"active"}`, version, name)
}

func productPath(id string) string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/catalogue/products/" + id
}

func grantCatalogue(t *testing.T, r *gin.Engine, subject, level string) {
	t.Helper()
	w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/"+subject, "editor", `{"level":"`+level+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestCataloguePersistsIdentityAndReplaysIdenticalWrite(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	access := do(r, "GET", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access", "editor", "")
	require.Equal(t, 200, access.Code, access.Body.String())
	require.JSONEq(t, `{"level":"editor"}`, access.Body.String())
	id := uuid.Must(uuid.NewV7()).String()
	body := productBody(0, "TEST Amoxicillin 500")
	w := do(r, "PUT", productPath(id), "editor", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	require.Equal(t, id, row["id"])
	require.Equal(t, float64(1), row["version"])
	require.Equal(t, "pending_review", row["regulatory_status"])
	require.Equal(t, float64(10), row["pack_size"])
	replay := do(r, "PUT", productPath(id), "editor", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	read := do(r, "GET", productPath(id), "editor", "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.JSONEq(t, w.Body.String(), read.Body.String())
}

func TestCatalogueIsolationVersionsAndAtomicHistory(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	id := uuid.Must(uuid.NewV7()).String()
	path := productPath(id)
	original := productBody(0, "TEST First revision")
	require.Equal(t, 200, do(r, "PUT", path, "editor", original).Code)
	for _, token := range []string{"sibling", "foreign"} {
		for _, suffix := range []string{"", "/revisions"} {
			require.Equal(t, 404, do(r, "GET", path+suffix, token, "").Code)
		}
		require.Equal(t, 404, do(r, "PUT", path, token, productBody(1, "TEST Attack")).Code)
	}
	require.Equal(t, 403, do(r, "GET", path, "denied", "").Code)
	require.Equal(t, 401, do(r, "GET", path, "unknown", "").Code)
	require.Equal(t, 404, do(r, "PUT", path, "reader", productBody(1, "TEST Reader change")).Code)
	require.Equal(t, 200, do(r, "GET", path, "reader", "").Code)
	require.Equal(t, 403, do(r, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "reader", `{"level":"editor"}`).Code)
	siblingPath := strings.Replace(path, pharmaOutlet, "0199e000-0000-7000-8000-000000000002", 1)
	require.Equal(t, 404, do(r, "GET", siblingPath, "editor", "").Code)
	require.Equal(t, 409, do(r, "PUT", path, "editor", productBody(0, "TEST Stale")).Code)
	updated := do(r, "PUT", path, "editor", productBody(1, "TEST Second revision"))
	require.Equal(t, 200, updated.Code, updated.Body.String())
	history := do(r, "GET", path+"/revisions", "reader", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var revisions struct {
		Data []struct {
			Version  int
			Snapshot map[string]any
		}
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &revisions))
	require.Len(t, revisions.Data, 2)
	require.Equal(t, 2, revisions.Data[0].Version)
	require.Equal(t, "TEST First revision", revisions.Data[1].Snapshot["brand_name"])
	require.Equal(t, "pending_review", revisions.Data[1].Snapshot["regulatory_status"])
	require.Equal(t, 409, do(r, "PUT", path, "editor", original).Code)
	require.Equal(t, 200, do(r, "PUT", path, "editor", productBody(1, "TEST Second revision")).Code)
	var counts struct {
		Events int
		Audits int
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.pharmacy.catalogue_updated.v1') AS events,
  (SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_catalogue_products' AND resource_id=?) AS audits`, id).Scan(&counts).Error
	}))
	require.Equal(t, 2, counts.Events)
	require.Equal(t, 2, counts.Audits)
	var event map[string]any
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var payload []byte
		if err := tx.Raw(`SELECT payload FROM outbox_events ORDER BY created_at LIMIT 1`).Row().Scan(&payload); err != nil {
			return err
		}
		return json.Unmarshal(payload, &event)
	}))
	require.NotContains(t, fmt.Sprint(event), "TEST First revision")
	require.NoError(t, db.Migrate(t.Context(), pharmacy.New().Migrations()))
}

func TestCatalogueUniquenessPaginationAndValidation(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	for i := 0; i < 5; i++ {
		id := uuid.Must(uuid.NewV7()).String()
		body := strings.Replace(productBody(0, "TEST Shared brand"), "TEST-AMOX-500", fmt.Sprintf("TEST-%d", i), 1)
		w := do(r, "PUT", productPath(id), "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	duplicate := strings.Replace(productBody(0, "TEST Duplicate"), "TEST-AMOX-500", " test-0 ", 1)
	require.Equal(t, 409, do(r, "PUT", productPath(uuid.Must(uuid.NewV7()).String()), "editor", duplicate).Code)
	base := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/catalogue/products"
	seen := map[string]bool{}
	cursor := ""
	for {
		w := do(r, "GET", base+"?limit=2&q=TEST&cursor="+url.QueryEscape(cursor), "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct{ ID string }
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		for _, row := range page.Data {
			require.False(t, seen[row.ID])
			seen[row.ID] = true
		}
		if !page.Page.HasMore {
			break
		}
		require.NotNil(t, page.Page.NextCursor)
		cursor = *page.Page.NextCursor
	}
	require.Len(t, seen, 5)
	w := do(r, "GET", base+"?q=%25", "editor", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"data":[]`)
	foreignCursor := pagination.Cursor{TenantID: testutil.TenantB, CreatedAt: time.Now(), ID: uuid.New()}.Encode()
	require.Equal(t, 400, do(r, "GET", base+"?cursor="+url.QueryEscape(foreignCursor), "editor", "").Code)
	for name, body := range map[string]string{
		"unknown regulation": strings.Replace(productBody(0, "TEST"), `"sku":`, `"schedule":"OTC","sku":`, 1),
		"missing version":    strings.Replace(productBody(0, "TEST"), `"expected_version":0,`, "", 1),
		"empty name":         productBody(0, "  "),
		"bad sku":            strings.Replace(productBody(0, "TEST"), "TEST-AMOX-500", "bad/code", 1),
		"zero strength":      strings.Replace(productBody(0, "TEST"), `"500"`, `"0.000"`, 1),
		"numeric strength":   strings.Replace(productBody(0, "TEST"), `"500"`, `500`, 1),
		"empty ingredients":  strings.Replace(productBody(0, "TEST"), `[{"name":"TEST amoxicillin","strength":"500","unit":"mg"}]`, `[]`, 1),
		"negative price":     strings.Replace(productBody(0, "TEST"), `12500`, `-1`, 1),
		"bad currency":       strings.Replace(productBody(0, "TEST"), `"INR"`, `"inr"`, 1),
		"empty pack":         strings.Replace(productBody(0, "TEST"), `"pack_size":10`, `"pack_size":0`, 1),
		"trailing json":      productBody(0, "TEST") + ` {}`,
		"too large":          productBody(0, strings.Repeat("x", 33000)),
	} {
		t.Run(name, func(t *testing.T) {
			w := do(r, "PUT", productPath(uuid.Must(uuid.NewV7()).String()), "editor", body)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	require.Equal(t, 404, do(r, "GET", productPath(uuid.NewString()), "editor", "").Code)
	require.Equal(t, 400, do(r, "GET", base+"?status=unknown", "editor", "").Code)
}

func TestCatalogueConcurrentRetryAndVersionConflict(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	path := productPath(uuid.Must(uuid.NewV7()).String())
	const workers = 8
	codes := make(chan int, workers)
	start := make(chan struct{})
	for range workers {
		go func() { <-start; codes <- do(r, "PUT", path, "editor", productBody(0, "TEST Retry")).Code }()
	}
	close(start)
	for range workers {
		require.Equal(t, 200, <-codes)
	}
	changes := make(chan int, workers)
	start = make(chan struct{})
	for i := range workers {
		go func() {
			<-start
			changes <- do(r, "PUT", path, "editor", productBody(1, fmt.Sprintf("TEST Concurrent %d", i))).Code
		}()
	}
	close(start)
	winners, conflicts := 0, 0
	for range workers {
		switch code := <-changes; code {
		case 200:
			winners++
		case 409:
			conflicts++
		default:
			t.Fatalf("unexpected %d", code)
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, workers-1, conflicts)
	w := do(r, "GET", path+"/revisions", "editor", "")
	var page struct{ Data []json.RawMessage }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
}

func catalogueTx(t *testing.T, db *tenantdb.DB, fn func(*gorm.DB) error) error {
	t.Helper()
	return db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func TestCatalogueRLSImmutableHistoryAndRevocation(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	path := productPath(id)
	require.Equal(t, 200, do(r, "PUT", path, "editor", productBody(0, "TEST Immutable")).Code)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("pharmacy_catalogue_products").Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count)
			return nil
		}))
	}
	for _, sql := range []string{
		`UPDATE pharmacy_catalogue_revisions SET version=99`,
		`DELETE FROM pharmacy_catalogue_revisions`,
		`DELETE FROM pharmacy_catalogue_products`,
		`UPDATE pharmacy_catalogue_products SET outlet_id='0199e000-0000-7000-8000-000000000002'`,
	} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(sql).Error }))
	}
	withdraw := strings.Replace(productBody(1, "TEST Immutable"), `"status":"active"`, `"status":"withdrawn"`, 1)
	require.Equal(t, 200, do(r, "PUT", path, "editor", withdraw).Code)
	require.Equal(t, 409, do(r, "PUT", path, "editor", productBody(2, "TEST Reactivated")).Code)
	// The same terminal-state rule must survive direct app-role SQL, not just handler validation.
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_catalogue_products SET status='active',version=version+1 WHERE id=?`, id).Error
	}))
	require.Equal(t, 200, do(r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-editor", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", path, "editor", "").Code)
	require.Equal(t, 404, do(r, "PUT", path, "editor", productBody(2, "TEST Revoked")).Code)
}

func TestCatalogueAccessInfrastructureFailureDeniesWith503(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_pharmacy_access_unavailable", SQL: `ALTER TABLE pharmacy_outlet_access RENAME TO test_unavailable_pharmacy_access`}}))
	w := do(r, "GET", productPath(uuid.Must(uuid.NewV7()).String()), "editor", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "test_unavailable")
}

func TestCatalogueOutboxReachesDurableNATSConsumer(t *testing.T) {
	r, db, bus, ctx := catalogueSetup(t)
	seedPharmacyOutlet(t, r)
	delivered := make(chan events.Event, 2)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "pharmacy-catalogue-observer", Subject: "helivanta.in.pharmacy.catalogue_updated.v1", Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error { delivered <- event; return nil }}}))
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", productPath(id), "editor", productBody(0, "TEST JetStream product")).Code)
	select {
	case event := <-delivered:
		require.Equal(t, testutil.TenantA, event.TenantID)
		require.Equal(t, "MedicineCatalogueUpdated", event.Type)
		require.JSONEq(t, fmt.Sprintf(`{"pharmacy_id":%q,"outlet_id":%q,"product_id":%q,"version":1,"status":"active"}`, pharmaOrganization, pharmaOutlet, id), string(event.Data))
	case <-ctx.Done():
		t.Fatal("catalogue event did not reach the durable JetStream consumer")
	}
}

func seedPharmacyOutlet(t *testing.T, r *gin.Engine) {
	t.Helper()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization
	w := do(r, "PUT", root, "editor", `{"code":"TEST-PHARMACY","name":"TEST Independent Pharmacy","ownership_type":"independent"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", root+"/outlets/"+pharmaOutlet, "editor", `{"code":"TEST-MAIN","name":"TEST Main Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
}
