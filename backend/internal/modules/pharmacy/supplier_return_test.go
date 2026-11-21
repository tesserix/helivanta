package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type supplierReturnWorld struct {
	r                              *gin.Engine
	db                             *tenantdb.DB
	bill, receipt, batch, location string
}

func newSupplierReturnWorld(t *testing.T, ownership string) supplierReturnWorld {
	return supplierReturnWorldWithDispatch(t, ownership, false)
}
func supplierReturnWorldWithDispatch(t *testing.T, ownership string, dispatch bool, readerPermissions ...authz.Permission) supplierReturnWorld {
	t.Helper()
	perms := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermAccountsRead, pharmacy.PermAccountsWrite, pharmacy.PermAccountsPayment, pharmacy.PermAccountsAdjust, pharmacy.PermSupplierReturnRead, pharmacy.PermSupplierReturnPrepare, pharmacy.PermSupplierReturnDispatch, pharmacy.PermLicenceManage, pharmacy.PermLicenceReview, pharmacy.PermScheduleDeclare, pharmacy.PermScheduleReview}
	var files *attachments.GCS
	if dispatch {
		cloud := testinfra.NewGCSProtocol(t)
		var err error
		files, err = attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
		require.NoError(t, err)
	}
	readPermissions := append([]authz.Permission{pharmacy.PermSupplierReturnRead}, readerPermissions...)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"editor": testutil.TenantA, "writer": testutil.TenantA, "reader": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA}, Perms: map[string][]authz.Permission{"editor": perms, "writer": perms, "reader": readPermissions, "sibling": perms, "foreign": perms}, Modules: []platform.Module{pharmacy.New()}, Documents: files, PharmacyEvidenceEnabled: dispatch})
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization
	created := do(r, "PUT", root, "editor", fmt.Sprintf(`{"code":"TEST-RETURN","name":"TEST Return Pharmacy","ownership_type":%q}`, ownership))
	require.Equal(t, 200, created.Code, created.Body.String())
	created = do(r, "PUT", root+"/outlets/"+pharmaOutlet, "editor", `{"code":"TEST-MAIN","name":"TEST Main Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	require.Equal(t, 200, created.Code, created.Body.String())
	for _, subject := range []string{"user-editor", "user-writer"} {
		grantCatalogue(t, r, subject, "editor")
	}
	grantCatalogue(t, r, "user-reader", "reader")
	product, location, batch := stockFixture(t, r)
	if dispatch {
		declaration := declare(t, r, product, "H", "editor")
		require.Equal(t, 200, review(r, product, declaration, "writer", "approved"))
		licence := uuid.Must(uuid.NewV7()).String()
		created := do(r, "PUT", licencePath("/licences/"+licence), "editor", licenceBody("2026-01-01", "2030-12-31"))
		require.Equal(t, 201, created.Code, created.Body.String())
		releaseLicenceEvidenceForTest(t, r, db, files, licence, "editor")
		approved := do(r, "POST", licencePath("/licences/"+licence+"/review"), "writer", `{"decision":"approved","reason":"TEST independent licence check"}`)
		require.Equal(t, 200, approved.Code, approved.Body.String())
	}
	w := supplierReturnWorld{r: r, db: db, bill: uuid.Must(uuid.NewV7()).String(), receipt: uuid.Must(uuid.NewV7()).String(), batch: batch, location: location}
	received := do(r, "PUT", stockPath("movements/"+w.receipt), "editor", stockBody(location, batch, "receipt", "TEST-original-supplier-delivery", 80))
	require.Equal(t, 200, received.Code, received.Body.String())
	invoice := fmt.Sprintf(`{"payee_reference":"TEST-EXT","payee_name":"TEST External Supplier","document_reference":"TEST-BILL-1","issued_on":"2026-11-01","due_on":"2026-11-30","currency":"INR","lines":[{"category":"inventory","receipt_id":%q,"description":"TEST received cartons","net_minor":10000,"tax_minor":1800,"recoverable_tax_minor":1800,"landed_minor":0}]}`, w.receipt)
	billed := do(r, "PUT", payableRoot()+"/payables/"+w.bill, "editor", invoice)
	require.Equal(t, 200, billed.Code, billed.Body.String())
	return w
}
func (w supplierReturnWorld) body(quantity int) string {
	return fmt.Sprintf(`{"payable_id":%q,"lines":[{"receipt_id":%q,"quarantine_location_id":%q,"quantity_units":%d,"expected_credit_minor":1500}],"reason":"TEST physical short-date review"}`, w.bill, w.receipt, w.location, quantity)
}
func (w supplierReturnWorld) request(method, path, actor, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+actor)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
	tenant := testutil.TenantA
	if actor == "foreign" {
		tenant = testutil.TenantB
	}
	req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	response := httptest.NewRecorder()
	w.r.ServeHTTP(response, req)
	return response
}
func supplierReturnPath(id string) string { return payableRoot() + "/supplier-returns/" + id }

func TestSupplierReturnDraftKeepsStockAndPaidBillsAndSupportsCancellation(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w := newSupplierReturnWorld(t, ownership)
			billPath := payableRoot() + "/payables/" + w.bill
			paid := do(w.r, "PUT", billPath+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(settlementBody("payment", "TEST fully paid", 11800, nil), "2026-10-15", "2026-11-15", 1))
			require.Equal(t, 200, paid.Code, paid.Body.String())
			id := uuid.Must(uuid.NewV7()).String()
			path := supplierReturnPath(id)
			input := w.body(4)
			created := w.request("PUT", path, "editor", input)
			require.Equal(t, 200, created.Code, created.Body.String())
			var receipt pharmacy.SupplierReturnCreationReceipt
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &receipt))
			require.EqualValues(t, 1, receipt.Number)
			require.Equal(t, id, receipt.ID.String())
			require.Equal(t, "TEST External Supplier", receipt.PayeeName)
			require.Equal(t, "TEST-EXT", receipt.PayeeReference)
			require.Equal(t, "INR", receipt.Currency)
			require.EqualValues(t, 1500, receipt.ExpectedCreditMinor)
			require.Len(t, receipt.Lines, 1)
			line := receipt.Lines[0]
			require.Equal(t, "TEST Quarantine product", line.ProductName)
			require.Equal(t, "TEST amoxicillin", line.GenericName)
			require.Equal(t, "capsule", line.UnitCode)
			require.Equal(t, "TEST-LOT", line.LotNumber)
			require.Equal(t, "2030-10-31", line.ExpiresOn)
			require.EqualValues(t, 80, line.ReceiptUnits)
			require.EqualValues(t, 4, *line.QuantityUnits)
			require.Equal(t, w.receipt, line.ReceiptID.String())
			for _, when := range []string{receipt.PreparedAt, line.SourceCreatedAt} {
				_, err := time.Parse(time.RFC3339Nano, when)
				require.NoError(t, err)
			}
			replay := w.request("PUT", path, "editor", input)
			require.Equal(t, 200, replay.Code, replay.Body.String())
			require.JSONEq(t, created.Body.String(), replay.Body.String())
			require.Equal(t, 409, w.request("PUT", path, "writer", input).Code)
			require.Equal(t, 409, w.request("PUT", path, "editor", w.body(5)).Code)
			viewed := w.request("GET", path, "reader", "")
			require.Equal(t, 200, viewed.Code, viewed.Body.String())
			require.Contains(t, viewed.Body.String(), `"state":"draft"`)
			require.Equal(t, "private, no-store", viewed.Header().Get("Cache-Control"))
			for _, actor := range []string{"sibling", "foreign"} {
				require.Equal(t, 404, w.request("GET", path, actor, "").Code)
				require.Equal(t, 404, w.request("PUT", path, actor, input).Code)
			}
			require.Equal(t, 403, w.request("PUT", path, "reader", input).Code)
			require.Equal(t, 403, w.request("GET", path, "denied", "").Code)
			cancelPath := path + "/cancellations/" + uuid.Must(uuid.NewV7()).String()
			cancelInput := `{"expected_version":0,"reason":"TEST supplier requests corrected quantities"}`
			cancelled := w.request("PUT", cancelPath, "writer", cancelInput)
			require.Equal(t, 200, cancelled.Code, cancelled.Body.String())
			recovered := w.request("GET", cancelPath, "writer", "")
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, cancelled.Body.String(), recovered.Body.String())
			require.Equal(t, 404, w.request("GET", cancelPath, "editor", "").Code)
			duplicate := w.request("PUT", cancelPath, "writer", cancelInput)
			require.Equal(t, 200, duplicate.Code, duplicate.Body.String())
			require.JSONEq(t, cancelled.Body.String(), duplicate.Body.String())
			require.Equal(t, 409, w.request("PUT", path+"/cancellations/"+uuid.Must(uuid.NewV7()).String(), "editor", cancelInput).Code)
			viewed = w.request("GET", path, "reader", "")
			require.Equal(t, 200, viewed.Code, viewed.Body.String())
			require.Contains(t, viewed.Body.String(), `"state":"cancelled"`)
			replay = w.request("PUT", path, "editor", input)
			require.Equal(t, 200, replay.Code, replay.Body.String())
			require.JSONEq(t, created.Body.String(), replay.Body.String())
			balance := do(w.r, "GET", stockPath("balance?location_id="+w.location+"&batch_id="+w.batch), "editor", "")
			require.Equal(t, 200, balance.Code, balance.Body.String())
			require.Contains(t, balance.Body.String(), `"on_hand_units":80`)
			bill := do(w.r, "GET", billPath, "editor", "")
			require.Equal(t, 200, bill.Code, bill.Body.String())
			var amounts struct {
				Paid        int64 `json:"paid_minor"`
				Credit      int64 `json:"credit_minor"`
				Outstanding int64 `json:"outstanding_minor"`
			}
			require.NoError(t, json.Unmarshal(bill.Body.Bytes(), &amounts))
			require.EqualValues(t, 11800, amounts.Paid)
			require.Zero(t, amounts.Credit)
			require.Zero(t, amounts.Outstanding)
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				var healthy bool
				if err := tx.Raw("SELECT pharmacy_operating_choice_integrity() AND pharmacy_operating_choice_support()").Scan(&healthy).Error; err != nil {
					return err
				}
				require.True(t, healthy)
				var count int64
				if err := tx.Table("pharmacy_supplier_returns").Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, count)
				if err := tx.Exec("SELECT set_config('app.pharmacy_operating_choices','TEST-choices',true)").Error; err != nil {
					return err
				}
				for _, table := range []string{"pharmacy_supplier_returns", "pharmacy_supplier_return_lines", "pharmacy_supplier_return_cancellations"} {
					if err := tx.Table(table).Count(&count).Error; err != nil {
						return err
					}
					require.Zero(t, count)
				}
				return nil
			}))
		})
	}
}

func TestSupplierReturnDraftRejectsReceiptAndCreditScopeAndRollsBackEvidence(t *testing.T) {
	w := newSupplierReturnWorld(t, "independent")
	path := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	for _, tc := range []struct {
		body   string
		status int
	}{
		{w.body(0), 400}, {w.body(81), 409},
		{strings.Replace(w.body(4), w.receipt, uuid.Must(uuid.NewV7()).String(), 1), 404},
		{strings.Replace(w.body(4), w.bill, uuid.Must(uuid.NewV7()).String(), 1), 404},
		{strings.Replace(w.body(4), `"expected_credit_minor":1500`, `"expected_credit_minor":-1`, 1), 400},
		{strings.Replace(w.body(4), `"reason":`, `"supplier_tenant_id":"forged","reason":`, 1), 400},
	} {
		response := w.request("PUT", path, "editor", tc.body)
		require.Equal(t, tc.status, response.Code, response.Body.String())
		require.Equal(t, 404, w.request("GET", path, "editor", "").Code)
	}
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_supplier_return_audit_failure")
	}
	t.Cleanup(clear)
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_supplier_return_audit_failure CHECK(resource_type<>'pharmacy_supplier_return_lines') NOT VALID")
	failed := w.request("PUT", path, "editor", w.body(4))
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic_supplier_return_audit_failure")
	require.Equal(t, 404, w.request("GET", path, "editor", "").Code)
	clear()
	created := w.request("PUT", path, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_supplier_return_audit_failure CHECK(resource_type<>'pharmacy_supplier_returns') NOT VALID")
	failed = w.request("GET", path, "reader", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
	clear()
	require.Equal(t, 200, w.request("GET", path, "reader", "").Code)
}

func TestSupplierReturnDraftDatabaseRejectsMissingAndForgedLineEvidence(t *testing.T) {
	w := newSupplierReturnWorld(t, "independent")
	missing := uuid.Must(uuid.NewV7()).String()
	insertHeader := func(tx *gorm.DB, id string, number int) error {
		return tx.Exec(`INSERT INTO pharmacy_supplier_returns(id,tenant_id,outlet_id,number,payable_id,payee_reference,payee_name,document_reference,currency,line_count,expected_credit_minor,reason,intent_sha256,actor_subject)
   VALUES(?,current_setting('app.tenant_id')::uuid,?,?,?,'TEST-EXT','TEST External Supplier','TEST-BILL-1','INR',1,1500,'TEST proof',?,'user-editor')`, id, pharmaOutlet, number, w.bill, strings.Repeat("a", 64)).Error
	}
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error { return insertHeader(tx, missing, 1) })
	require.ErrorContains(t, err, "complete bounded receipt lines")
	require.Equal(t, 404, w.request("GET", supplierReturnPath(missing), "editor", "").Code)
	id := uuid.Must(uuid.NewV7()).String()
	created := w.request("PUT", supplierReturnPath(id), "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	forged := uuid.Must(uuid.NewV7()).String()
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := insertHeader(tx, forged, 2); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_supplier_return_lines(id,tenant_id,outlet_id,return_id,position,receipt_id,quarantine_location_id,quantity_units,expected_credit_minor,batch_id,product_id,product_version,product_name,generic_name,unit_code,lot_number,expires_on,receipt_units,source_reference,source_created_at)
   SELECT ?,tenant_id,outlet_id,?,position,receipt_id,quarantine_location_id,quantity_units,expected_credit_minor,batch_id,product_id,product_version,'TEST forged identity',generic_name,unit_code,lot_number,expires_on,receipt_units,source_reference,source_created_at
   FROM pharmacy_supplier_return_lines WHERE tenant_id=current_setting('app.tenant_id')::uuid AND outlet_id=? AND return_id=?`, uuid.Must(uuid.NewV7()), forged, pharmaOutlet, id).Error
	})
	require.ErrorContains(t, err, "exact external receipt evidence")
	require.Equal(t, 404, w.request("GET", supplierReturnPath(forged), "editor", "").Code)
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE pharmacy_supplier_returns SET reason='TEST erased evidence' WHERE tenant_id=current_setting('app.tenant_id')::uuid AND outlet_id=? AND id=?", pharmaOutlet, id).Error
	})
	require.Error(t, err)
	replay := w.request("PUT", supplierReturnPath(id), "editor", w.body(4))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
}

func TestSupplierReturnHistoryIsBoundedScopedAndAudited(t *testing.T) {
	w := newSupplierReturnWorld(t, "independent")
	root := payableRoot() + "/supplier-returns"
	var newest string
	for range 3 {
		newest = uuid.Must(uuid.NewV7()).String()
		response := w.request("PUT", supplierReturnPath(newest), "editor", w.body(4))
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	cancelled := w.request("PUT", supplierReturnPath(newest)+"/cancellations/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"expected_version":0,"reason":"TEST cancelled history"}`)
	require.Equal(t, 200, cancelled.Code, cancelled.Body.String())
	first := w.request("GET", root+"?limit=2", "reader", "")
	require.Equal(t, 200, first.Code, first.Body.String())
	require.Equal(t, "private, no-store", first.Header().Get("Cache-Control"))
	var page struct {
		Data []pharmacy.SupplierReturnSummary `json:"data"`
		Page struct {
			Next *string `json:"next_cursor"`
			More bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.True(t, page.Page.More)
	require.NotNil(t, page.Page.Next)
	require.EqualValues(t, 3, page.Data[0].Number)
	require.Equal(t, "TEST External Supplier", page.Data[0].PayeeName)
	require.Equal(t, "cancelled", page.Data[0].State)
	require.EqualValues(t, 1, page.Data[0].Version)
	require.NotContains(t, first.Body.String(), "actor_subject")
	second := w.request("GET", root+"?limit=2&cursor="+*page.Page.Next, "reader", "")
	require.Equal(t, 200, second.Code, second.Body.String())
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.EqualValues(t, 1, page.Data[0].Number)
	require.False(t, page.Page.More)
	require.Nil(t, page.Page.Next)
	for _, actor := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, w.request("GET", root, actor, "").Code)
	}
	require.Equal(t, 403, w.request("GET", root, "denied", "").Code)
	for _, query := range []string{"?limit=201", "?limit=1&limit=2", "?other=all", "?cursor=bad"} {
		response := w.request("GET", root+query, "reader", "")
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: uuid.Must(uuid.NewV7()).String(), SQL: "ALTER TABLE audit_events ADD CONSTRAINT synthetic_return_history_audit_failure CHECK(resource_type<>'pharmacy_supplier_returns' OR action<>'list_read') NOT VALID"}}))
	t.Cleanup(func() {
		require.NoError(t, w.db.Migrate(context.Background(), []tenantdb.Migration{{ID: uuid.Must(uuid.NewV7()).String(), SQL: "ALTER TABLE audit_events DROP CONSTRAINT synthetic_return_history_audit_failure"}}))
	})
	failed := w.request("GET", root, "reader", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
}
