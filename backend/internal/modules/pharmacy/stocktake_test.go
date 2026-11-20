package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func stocktakeHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	write := []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.correct", "pharmacy.stock.release", "pharmacy.stock.quarantine"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "reviewer": testutil.TenantA, "reader": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"editor": write, "reviewer": write, "sibling": write, "foreign": write,
			"reader": {"pharmacy.catalogue.read", "pharmacy.stock.read"},
		},
		Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reviewer", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	return r, db
}

func TestStocktakeSnapshotIsFrozenAndOutletScoped(t *testing.T) {
	r, _ := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	receipt := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-stocktake-opening", 10))
	require.Equal(t, 200, receipt.Code, receipt.Body.String())
	id := uuid.Must(uuid.NewV7()).String()
	path := outletPath("/stock/stocktakes/") + id
	input := fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST cycle count"}`, location)
	created := do(r, "PUT", path, "editor", input)
	require.Equal(t, 200, created.Code, created.Body.String())
	var take struct {
		ID           string
		Number       int64
		Version      int64
		State        string
		SnapshotRows int64 `json:"snapshot_rows"`
		CountedRows  int64 `json:"counted_rows"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &take))
	require.Equal(t, id, take.ID)
	require.Equal(t, int64(1), take.Number)
	require.Zero(t, take.Version)
	require.Equal(t, "counting", take.State)
	require.Equal(t, int64(1), take.SnapshotRows)
	require.Zero(t, take.CountedRows)
	readLines := func() {
		t.Helper()
		res := do(r, "GET", path+"/lines?limit=1", "reader", "")
		require.Equal(t, 200, res.Code, res.Body.String())
		var body struct {
			Data []struct {
				BatchID      string `json:"batch_id"`
				LocationID   string `json:"location_id"`
				BookUnits    int64  `json:"book_units"`
				CountedUnits *int64 `json:"counted_units"`
				Unit         string `json:"unit_of_issue"`
			}
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
		require.Len(t, body.Data, 1)
		require.Equal(t, batch, body.Data[0].BatchID)
		require.Equal(t, location, body.Data[0].LocationID)
		require.Equal(t, int64(10), body.Data[0].BookUnits)
		require.Nil(t, body.Data[0].CountedUnits)
		require.Equal(t, "capsule", body.Data[0].Unit)
	}
	readLines()
	later := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-after-stocktake-start", 5))
	require.Equal(t, 200, later.Code, later.Body.String())
	replay := do(r, "PUT", path, "editor", input)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	readLines()
	require.Equal(t, 409, do(r, "PUT", path, "reviewer", input).Code)
	require.Equal(t, 409, do(r, "PUT", path, "editor", strings.Replace(input, "TEST cycle count", "Changed intent", 1)).Code)
	require.Equal(t, 409, do(r, "PUT", outletPath("/stock/stocktakes/")+uuid.Must(uuid.NewV7()).String(), "editor", input).Code)
	for _, token := range []string{"sibling", "foreign"} {
		for _, suffix := range []string{"", "/lines"} {
			require.Equal(t, 404, do(r, "GET", path+suffix, token, "").Code)
		}
	}
	require.Equal(t, 403, do(r, "PUT", path, "reader", input).Code)
	require.Equal(t, 403, do(r, "GET", path, "denied", "").Code)
	require.Equal(t, 401, do(r, "GET", path, "unknown", "").Code)
}
