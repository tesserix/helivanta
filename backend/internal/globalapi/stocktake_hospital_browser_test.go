package globalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func hospitalStocktakeBrowserOrigin(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") {
		return fmt.Errorf("hospital stocktake browser requires a disposable loopback shell")
	}
	return nil
}

func TestHospitalStocktakeBrowserRequiresLoopbackShell(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:4344", "http://localhost:4344/"} {
		require.NoError(t, hospitalStocktakeBrowserOrigin(origin))
	}
	for _, origin := range []string{"", "https://helivanta.com", "http://localhost.attacker.example", "http://user:password@localhost", "http://localhost/path", "http://localhost/?token=value", "http://localhost/#fragment", "file:///tmp/shell"} {
		require.Error(t, hospitalStocktakeBrowserOrigin(origin))
	}
}

func hospitalStocktakeBrowserAcceptance(t *testing.T, db *tenantdb.DB, gateway http.Handler, tokens map[string]string, hospital, department, owner, counter, reviewer string) func(string, string) {
	t.Helper()
	appURL := os.Getenv("HELIVANTA_HOSPITAL_STOCKTAKE_BROWSER_URL")
	if appURL == "" {
		return nil
	}
	require.NoError(t, hospitalStocktakeBrowserOrigin(appURL))
	return func(pharmacy, outlet string) {
		t.Helper()
		server := httptest.NewServer(gateway)
		defer server.Close()
		accounts := map[string]map[string]string{}
		for actor, subject := range map[string]string{"owner": owner, "receiver": counter, "stock-reviewer": reviewer} {
			require.NotEmpty(t, tokens[subject])
			accounts[actor] = map[string]string{"subject": subject, "token": tokens[subject]}
		}
		material, err := json.Marshal(map[string]any{
			"app_url": appURL, "gateway_url": server.URL, "tenant_id": testutil.TenantA,
			"hospital": hospital, "department": department, "pharmacy": pharmacy, "outlet": outlet,
			"accounts": accounts, "unassigned_actors": []string{"owner"},
			"take_number": 2, "book_units": 32, "counted_units": 29,
			"quarantine_name": "Synthetic hospital quarantine · SYN-HOSPITAL-Q",
			"outlet_name":     "Synthetic hospital stocktake pharmacy · Synthetic hospital counting outlet · SYN-HOSPITAL-COUNT",
		})
		require.NoError(t, err)
		root, err := filepath.Abs("../../..")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-stocktake-worker.mjs"))
		command.Dir = root
		command.Stdin = bytes.NewReader(material)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "hospital stocktake browser acceptance: %s", output)
		t.Log(string(output))
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject',?,true)`, pharmacy, outlet, owner).Error; err != nil {
				return err
			}
			var ledger struct{ Corrections, Units int64 }
			if err := tx.Raw(`SELECT count(*)FILTER(WHERE kind='correction')corrections,COALESCE(sum(quantity_units),0)units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, uuid.MustParse(outlet)).Scan(&ledger).Error; err != nil {
				return err
			}
			require.EqualValues(t, 2, ledger.Corrections, "the browser posts exactly one additional correction")
			require.EqualValues(t, 29, ledger.Units)
			return nil
		}))
	}
}
