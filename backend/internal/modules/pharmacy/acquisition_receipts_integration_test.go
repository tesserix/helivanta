package pharmacy_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // owning module public interface under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func acquisitionRead(r *gin.Engine, path, token, tenant, subject string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	req.Header.Set("X-Helivanta-Expected-Subject", subject)
	r.ServeHTTP(w, req)
	return w
}
func acquisitionPage(t *testing.T, r *gin.Engine, path string) []pharmacy.AcquisitionReceipt {
	t.Helper()
	response := acquisitionRead(r, path, "finance", testutil.TenantA, "user-finance")
	require.Equal(t, 200, response.Code, response.Body.String())
	var page struct{ Data []pharmacy.AcquisitionReceipt }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	return page.Data
}
func TestFinancialAcquisitionReceiptsRequireFinanceAndOwnedOutletWithoutStockPermission(t *testing.T) {
	r, db := catalogueHarness(t)
	bad, err := db.LintRLS(t.Context())
	require.NoError(t, err)
	require.Empty(t, bad, "financial read scope must preserve the forced tenant RLS contract")
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-finance", "reader")
	_, location, batch := stockFixture(t, r)
	receipt := uuid.Must(uuid.NewV7()).String()
	root := strings.Split(productPath(""), "/catalogue")[0]
	recorded := do(r, "PUT", root+"/stock/movements/"+receipt, "editor", stockBody(location, batch, "receipt", "TEST-ACQUISITION", 20))
	require.Equal(t, 200, recorded.Code, recorded.Body.String())
	path := payableRoot() + "/acquisition-receipts?limit=1"
	rows := acquisitionPage(t, r, path)
	require.Len(t, rows, 1)
	require.Equal(t, receipt, rows[0].ID.String())
	require.Equal(t, testutil.TenantA, rows[0].OwnerTenantID.String())
	require.Equal(t, pharmaOutlet, rows[0].OutletID.String())
	require.Equal(t, "INR", rows[0].Currency)
	require.EqualValues(t, 20, rows[0].QuantityUnits)
	require.Nil(t, rows[0].Supplier)
	require.Nil(t, rows[0].PayableID)
	require.Equal(t, 403, do(r, "GET", root+"/stock/movements", "finance", "").Code)
	require.Equal(t, 403, acquisitionRead(r, path, "reader", testutil.TenantA, "user-reader").Code)
	require.Equal(t, 404, acquisitionRead(r, path, "sibling", testutil.TenantA, "user-sibling").Code)
	require.Equal(t, 404, acquisitionRead(r, path, "foreign", testutil.TenantB, "user-foreign").Code)
	require.Equal(t, 409, acquisitionRead(r, path, "finance", testutil.TenantB, "user-finance").Code)
	require.Equal(t, 409, acquisitionRead(r, path, "finance", testutil.TenantA, "user-other").Code)
	require.Equal(t, 409, do(r, "GET", path, "finance", "").Code)
	require.Equal(t, 400, acquisitionRead(r, payableRoot()+"/acquisition-receipts?unexpected=value", "finance", testutil.TenantA, "user-finance").Code)
	require.Equal(t, 400, acquisitionRead(r, payableRoot()+"/acquisition-receipts?limit=51", "finance", testutil.TenantA, "user-finance").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("audit_events").Where("tenant_id=? AND actor_subject=? AND action=? AND resource_id=?", testutil.TenantA, "user-finance", "pharmacy_acquisition_receipts_read", pharmaOutlet).Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, n)
		return nil
	}))
}
func assertSupplierAcquisition(t *testing.T, r *gin.Engine, db *tenantdb.DB, root string, receipt uuid.UUID, owner string, accepted int64, closed bool) {
	t.Helper()
	rows := acquisitionPage(t, r, root+"/accounts/acquisition-receipts?limit=50")
	require.Len(t, rows, 1)
	row := rows[0]
	assertFinanceCannotMutateReceiving(t, db, row.OutletID, receipt)
	require.Equal(t, receipt, row.ID)
	require.NotNil(t, row.Supplier)
	require.Equal(t, owner, row.Supplier.SourceProduct)
	require.EqualValues(t, 20, row.Supplier.OriginalAcceptedUnits)
	require.Equal(t, accepted, row.Supplier.EffectiveAcceptedUnits)
	require.Equal(t, closed, row.Supplier.ReceivingClosed)
	// Setting a purpose alone cannot impersonate the finance account.
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-finance',true),set_config('app.pharmacy_acquisition_read','authorized',true),set_config('app.pharmacy_acquisition_outlet',?,true)`, row.OutletID.String()).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Table("pharmacy_supplier_receipts").Where("tenant_id=? AND id=?", testutil.TenantA, receipt).Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n, "a forged finance purpose without matching actor/outlet lens must deny")
		return nil
	}))
}

func assertFinanceCannotMutateReceiving(t *testing.T, db *tenantdb.DB, outlet, receipt uuid.UUID) {
	t.Helper()
	ctx := tenantdb.WithActor(t.Context(), "user-finance")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-finance',true),set_config('app.pharmacy_acquisition_read','authorized',true),set_config('app.pharmacy_acquisition_outlet',?,true)`, outlet.String()).Error; err != nil {
			return err
		}
		var visible int64
		if err := tx.Table("pharmacy_supplier_receipts").Where("tenant_id=? AND id=?", testutil.TenantA, receipt).Count(&visible).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, visible)
		result := tx.Exec(`UPDATE pharmacy_supplier_receiving_heads SET updated_at=updated_at WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, outlet)
		require.NoError(t, result.Error)
		require.Zero(t, result.RowsAffected, "a finance-only read purpose must not admit updates")
		return nil
	}))
}
