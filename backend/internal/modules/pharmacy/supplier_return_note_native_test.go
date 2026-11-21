package pharmacy_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplierReturnNoteRetainsNamesAndReauthorizesEveryDownload(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w := supplierReturnWorldWithDispatch(t, ownership, true)
			parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
			created := w.request("PUT", parent, "editor", w.body(4))
			require.Equal(t, 200, created.Code, created.Body.String())
			path := parent + "/note.html"
			require.Equal(t, 404, w.request("GET", path, "reader", "").Code)
			posted := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
			require.Equal(t, 200, posted.Code, posted.Body.String())
			first := w.request("GET", path, "reader", "")
			require.Equal(t, 200, first.Code, first.Body.String())
			require.Equal(t, "private, no-store", first.Header().Get("Cache-Control"))
			require.Equal(t, "text/html; charset=utf-8", first.Header().Get("Content-Type"))
			require.Equal(t, `attachment; filename="supplier-return-1.html"`, first.Header().Get("Content-Disposition"))
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(first.Body.Bytes())), first.Header().Get("X-Export-Sha256"))
			require.Equal(t, strconv.Itoa(first.Body.Len()), first.Header().Get("X-Export-Byte-Count"))
			require.Equal(t, "1", first.Header().Get("X-Export-Row-Count"))
			require.Contains(t, first.Header().Get("Content-Security-Policy"), "sandbox")
			for _, text := range []string{"TEST Return Pharmacy", "TEST Main Outlet", "TEST External Supplier", "TEST-BILL-1", "Not a tax invoice"} {
				require.Contains(t, first.Body.String(), text)
			}
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				if err := tx.Exec("UPDATE pharmacy_outlets SET name='TEST renamed outlet' WHERE id=?", pharmaOutlet).Error; err != nil {
					return err
				}
				return tx.Exec("UPDATE pharmacy_organizations SET name='TEST renamed pharmacy' WHERE id=?", pharmaOrganization).Error
			}))
			repeated := w.request("GET", path, "reader", "")
			require.Equal(t, 200, repeated.Code, repeated.Body.String())
			require.Equal(t, first.Body.String(), repeated.Body.String())
			for _, actor := range []string{"foreign", "sibling"} {
				denied := w.request("GET", path, actor, "")
				require.Equal(t, 404, denied.Code, denied.Body.String())
				require.Empty(t, denied.Header().Get("Content-Disposition"))
			}
			require.Equal(t, 400, w.request("GET", path+"?other=all", "reader", "").Code)
			require.Equal(t, 400, w.request("GET", path, "reader", "{}").Code)
			revoked := do(w.r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "")
			require.Equal(t, 200, revoked.Code, revoked.Body.String())
			denied := w.request("GET", path, "reader", "")
			require.Equal(t, 404, denied.Code, denied.Body.String())
			require.Empty(t, denied.Header().Get("Content-Disposition"))
		})
	}
}

func TestSupplierReturnNoteRefusesUnauditedAndHistoricalMissingSnapshotBytes(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_note_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	apply("ALTER TABLE pharmacy_supplier_return_dispatches DISABLE TRIGGER pharmacy_supplier_return_note_snapshot")
	posted := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
	apply("ALTER TABLE pharmacy_supplier_return_dispatches ENABLE TRIGGER pharmacy_supplier_return_note_snapshot")
	require.Equal(t, 200, posted.Code, posted.Body.String())
	missing := w.request("GET", parent+"/note.html", "reader", "")
	require.Equal(t, 503, missing.Code, missing.Body.String())
	require.Empty(t, missing.Header().Get("Content-Disposition"))
	require.NotContains(t, missing.Body.String(), "TEST External Supplier")
	parent = supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created = w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	posted = w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", dispatchInput(t, w))
	require.Equal(t, 200, posted.Code, posted.Body.String())
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_note_audit_failure CHECK(resource_type<>'pharmacy_supplier_return_note') NOT VALID")
	t.Cleanup(func() { apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_note_audit_failure") })
	failed := w.request("GET", parent+"/note.html", "reader", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.Empty(t, failed.Header().Get("Content-Disposition"))
	require.NotContains(t, failed.Body.String(), "TEST External Supplier")
}
