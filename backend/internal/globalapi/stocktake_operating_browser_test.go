//go:build stocktakebrowser

package globalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestStocktakeReviewedBrowser(t *testing.T) {
	appURL := os.Getenv("HELIVANTA_OPERATING_BROWSER_URL")
	require.NotEmpty(t, appURL, "the hosted built shell must be running")
	f := newPharmacyReceiverFixtureWithAdmission(t, nil, organization.New())
	for _, actor := range []string{"owner", "receiver"} {
		require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, f.call("GET", "/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20", actor, nil, 200))
	}
	f.activate(t)
	batch := v7()
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-BROWSER-COUNT"), 200)
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/receipts/"+v7()), "receiver", f.receipt(batch, f.quarantine, "TEST browser stocktake receipt"), 200)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	activateReviewedStocktakeReviewer(t, &f)

	// Only the release snapshot uses this fixture engine; all business requests
	// traverse the real Global API and current IAM/Workforce/Pharmacy owners.
	snapshot := gin.New()
	testutil.MountBrowserReleaseCapabilities(t, snapshot, testutil.StaticVerifier{
		"owner": testutil.TenantA, "reviewer": testutil.TenantA,
		"receiver": testutil.TenantA, "stock-reviewer": testutil.TenantA,
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/workspace/release-capabilities" {
			snapshot.ServeHTTP(w, r)
			return
		}
		f.handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	material, err := json.Marshal(map[string]any{
		"app_url": appURL, "gateway_url": server.URL, "tenant_id": testutil.TenantA,
		"pharmacy": f.pharmacy, "outlet": f.outlet,
	})
	require.NoError(t, err)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-stocktake-worker.mjs"))
	command.Dir = root
	command.Stdin = bytes.NewReader(material)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "reviewed stocktake browser acceptance: %s", output)
	t.Log(string(output))
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections,COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, ledger.Corrections)
		require.EqualValues(t, 27, ledger.Units)
		return nil
	})
}
