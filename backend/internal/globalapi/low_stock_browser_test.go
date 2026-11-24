//go:build lowstockbrowser

package globalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestLowStockBuiltBrowser(t *testing.T) {
	appURL := os.Getenv("HELIVANTA_LOW_STOCK_BROWSER_URL")
	require.NotEmpty(t, appURL, "the hosted built shell must be running")
	for _, ownership := range []string{"independent", "hospital_owned"} {
		t.Run(ownership, func(t *testing.T) {
			f := newLowStockGatewayFixture(t, ownership)
			server := httptest.NewServer(f.handler)
			t.Cleanup(server.Close)
			material, err := json.Marshal(map[string]any{
				"app_url": appURL, "gateway_url": server.URL, "tenant_id": testutil.TenantA,
				"pharmacy": f.pharmacy, "outlet": f.outlet, "product": f.product,
				"tokens": f.tokens, "ownership": ownership,
			})
			require.NoError(t, err)
			root, err := filepath.Abs("../../..")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-low-stock.mjs"))
			command.Dir = root
			command.Stdin = bytes.NewReader(material)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "built low-stock browser acceptance: %s", output)
			t.Log(string(output))
			require.NoError(t, f.db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.pharmacy_subject','user-owner',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)", f.outlet, f.pharmacy).Error; err != nil {
					return err
				}
				var revisions int64
				if err := tx.Table("pharmacy_reorder_levels").Where("tenant_id=? AND outlet_id=? AND product_id=? AND actor_subject=?", testutil.TenantA, f.outlet, f.product, "user-owner").Count(&revisions).Error; err != nil {
					return err
				}
				require.EqualValues(t, 2, revisions, "authorized history contains exactly the enable and disable revisions")
				var movements int64
				if err := tx.Table("pharmacy_stock_movements").Where("tenant_id=? AND outlet_id=?", testutil.TenantA, f.outlet).Count(&movements).Error; err != nil {
					return err
				}
				require.Zero(t, movements, "minimum administration must not move physical stock")
				return nil
			}))
		})
	}
}
