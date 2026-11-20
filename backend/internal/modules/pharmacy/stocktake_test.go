package pharmacy_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func stocktakeHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	write := []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.correct", "pharmacy.stock.release", "pharmacy.stock.quarantine"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "reviewer": testutil.TenantA, "approver": testutil.TenantA, "reader": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"editor": write, "reviewer": write, "approver": write, "sibling": write, "foreign": write,
			"reader": {"pharmacy.catalogue.read", "pharmacy.stock.read"},
		},
		Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reviewer", "editor")
	grantCatalogue(t, r, "user-approver", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	return r, db
}

func TestStocktakeIndependentApprovalPreservesLaterMovements(t *testing.T) {
	r, db := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	received := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-reviewed-count-opening", 10))
	require.Equal(t, 200, received.Code, received.Body.String())
	path := outletPath("/stock/stocktakes/") + uuid.Must(uuid.NewV7()).String()
	start := fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST reviewed count"}`, location)
	created := do(r, "PUT", path, "editor", start)
	require.Equal(t, 200, created.Code, created.Body.String())
	lines := do(r, "GET", path+"/lines", "reader", "")
	require.Equal(t, 200, lines.Code, lines.Body.String())
	var list struct{ Data []struct{ ID string } }
	require.NoError(t, json.Unmarshal(lines.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	input := fmt.Sprintf(`{"expected_version":0,"counts":[{"line_id":%q,"counted_units":8}],"reason":"TEST physical observation"}`, list.Data[0].ID)
	counted := do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "reviewer", input)
	require.Equal(t, 200, counted.Code, counted.Body.String())
	// An accepted observation precedes this legitimate receipt. Approval must
	// apply its difference to fifteen, preserving the five later received units.
	later := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-reviewed-count-later", 5))
	require.Equal(t, 200, later.Code, later.Body.String())
	submitPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	submitInput := `{"decision":"submit","expected_version":1,"reason":"TEST all shelves counted"}`
	submitted := do(r, "PUT", submitPath, "editor", submitInput)
	require.Equal(t, 200, submitted.Code, submitted.Body.String())
	require.Equal(t, 409, do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "reviewer", strings.Replace(input, `"expected_version":0`, `"expected_version":2`, 1)).Code)
	approvalPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	approvalInput := `{"decision":"approve","expected_version":2,"extra_units_disposition":"no_extra_units","reason":"TEST independently reviewed shortage"}`
	for _, actor := range []string{"editor", "reviewer"} {
		require.Equal(t, 409, do(r, "PUT", approvalPath, actor, approvalInput).Code)
	}
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_stocktake_commands'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.stock_movement_recorded.v1'"},
	} {
		apply := func(sql string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_stocktake_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() {
			apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_stocktake_evidence_failure")
		}
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_stocktake_evidence_failure CHECK(" + fault.check + ") NOT VALID")
		failed := do(r, "PUT", approvalPath, "approver", approvalInput)
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "synthetic_stocktake_evidence_failure")
		require.Equal(t, 404, do(r, "GET", approvalPath, "reader", "").Code)
		unchanged := do(r, "GET", path, "reader", "")
		require.Equal(t, 200, unchanged.Code, unchanged.Body.String())
		var pending struct {
			State   string
			Version int64
		}
		require.NoError(t, json.Unmarshal(unchanged.Body.Bytes(), &pending))
		require.Equal(t, "submitted", pending.State)
		require.Equal(t, int64(2), pending.Version)
		balance := do(r, "GET", outletPath("/stock/balance?location_id=")+location+"&batch_id="+batch, "reader", "")
		require.Equal(t, 200, balance.Code, balance.Body.String())
		var units struct {
			OnHand int64 `json:"on_hand_units"`
		}
		require.NoError(t, json.Unmarshal(balance.Body.Bytes(), &units))
		require.Equal(t, int64(15), units.OnHand)
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			var events int64
			if err := tx.Table("outbox_events").Where("subject='helivanta.in.pharmacy.stock_movement_recorded.v1'").Count(&events).Error; err != nil {
				return err
			}
			require.Equal(t, int64(2), events)
			return nil
		}))
		clear()
	}
	approved := do(r, "PUT", approvalPath, "approver", approvalInput)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	var decision struct {
		State      string
		Version    int64
		RecordedAt string `json:"recorded_at"`
	}
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &decision))
	require.Equal(t, "posted", decision.State)
	require.Equal(t, int64(3), decision.Version)
	_, err := time.Parse(time.RFC3339Nano, decision.RecordedAt)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		replayed := do(r, "PUT", approvalPath, "approver", approvalInput)
		require.Equal(t, 200, replayed.Code, replayed.Body.String())
		require.JSONEq(t, approved.Body.String(), replayed.Body.String())
	}
	recovered := do(r, "GET", submitPath, "reader", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, submitted.Body.String(), recovered.Body.String())
	balance := do(r, "GET", outletPath("/stock/balance?location_id=")+location+"&batch_id="+batch, "reader", "")
	require.Equal(t, 200, balance.Code, balance.Body.String())
	var stock struct {
		Units int64 `json:"on_hand_units"`
	}
	require.NoError(t, json.Unmarshal(balance.Body.Bytes(), &stock))
	require.Equal(t, int64(13), stock.Units)
	exported := do(r, "GET", path+"/register.csv", "reader", "")
	require.Equal(t, 200, exported.Code, exported.Body.String())
	require.Contains(t, exported.Header().Get("Content-Type"), "text/csv")
	require.Equal(t, "no-store", exported.Header().Get("Cache-Control"))
	records, err := csv.NewReader(strings.NewReader(exported.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	register := map[string]string{}
	for i, field := range records[0] {
		register[field] = records[1][i]
	}
	require.Equal(t, "1", register["stocktake_number"])
	require.Equal(t, "posted", register["stocktake_state"])
	require.Equal(t, "3", register["stocktake_version"])
	require.Equal(t, "10", register["book_units"])
	require.Equal(t, "8", register["counted_units"])
	require.Equal(t, "-2", register["difference_units"])
	require.Equal(t, "-2", register["adjustment_units"])
	require.Equal(t, "unknown", register["register_section"])
	require.Equal(t, "user-reviewer", register["counted_by_reference"])
	require.Equal(t, "user-approver", register["approved_by_reference"])
	movement, err := uuid.Parse(register["adjustment_movement_id"])
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), movement.Version())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("outbox_events").Where("payload->'data'->>'movement_id'=?", movement.String()).Count(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count)
		return nil
	}))
	positions := do(r, "GET", outletPath("/stock/positions.csv?as_of=")+decision.RecordedAt, "reader", "")
	require.Equal(t, 200, positions.Code, positions.Body.String())
	parts, err := csv.NewReader(strings.NewReader(positions.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, parts, 2)
	position := map[string]string{}
	for i, field := range parts[0] {
		position[field] = parts[1][i]
	}
	require.Equal(t, "13", position["on_hand_units"])
	require.Equal(t, "-2", position["correction_units"])
	for _, actor := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "GET", path+"/register.csv", actor, "").Code)
	}
	require.Equal(t, 403, do(r, "GET", path+"/register.csv", "denied", "").Code)
	require.Equal(t, 409, do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "approver", strings.Replace(approvalInput, `"expected_version":2`, `"expected_version":3`, 1)).Code)
	creationReplay := do(r, "PUT", path, "editor", start)
	require.Equal(t, 200, creationReplay.Code, creationReplay.Body.String())
	require.JSONEq(t, created.Body.String(), creationReplay.Body.String())
	nextPath := outletPath("/stock/stocktakes/") + uuid.Must(uuid.NewV7()).String()
	next := do(r, "PUT", nextPath, "editor", start)
	require.Equal(t, 200, next.Code, next.Body.String())
	cancelled := do(r, "PUT", nextPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"cancel","expected_version":0,"reason":"TEST retain abandoned count"}`)
	require.Equal(t, 200, cancelled.Code, cancelled.Body.String())
	require.Equal(t, 200, do(r, "PUT", outletPath("/stock/stocktakes/")+uuid.Must(uuid.NewV7()).String(), "editor", start).Code)
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

func TestStocktakeCountsAreVersionedAndMovementBound(t *testing.T) {
	r, _ := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	receipt := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-count-opening", 10))
	require.Equal(t, 200, receipt.Code, receipt.Body.String())
	path := outletPath("/stock/stocktakes/") + uuid.Must(uuid.NewV7()).String()
	start := fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST count"}`, location)
	created := do(r, "PUT", path, "editor", start)
	require.Equal(t, 200, created.Code, created.Body.String())
	lines := do(r, "GET", path+"/lines", "reader", "")
	require.Equal(t, 200, lines.Code, lines.Body.String())
	var list struct {
		Data []struct{ ID string }
	}
	require.NoError(t, json.Unmarshal(lines.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	input := fmt.Sprintf(`{"expected_version":0,"counts":[{"line_id":%q,"counted_units":8}],"reason":"TEST shelf observation"}`, list.Data[0].ID)
	command := path + "/counts/" + uuid.Must(uuid.NewV7()).String()
	counted := do(r, "PUT", command, "editor", input)
	require.Equal(t, 200, counted.Code, counted.Body.String())
	var saved struct {
		Version int64
		State   string
	}
	require.NoError(t, json.Unmarshal(counted.Body.Bytes(), &saved))
	require.Equal(t, int64(1), saved.Version)
	require.Equal(t, "counting", saved.State)
	current := do(r, "GET", path, "reader", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	var progress struct {
		Version                int64
		CountedRows            int64  `json:"counted_rows"`
		DifferenceRows         int64  `json:"difference_rows"`
		PositiveDifferenceRows *int64 `json:"positive_difference_rows"`
		ReviewRelationship     string `json:"review_relationship"`
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &progress))
	require.Equal(t, int64(1), progress.Version)
	require.Equal(t, int64(1), progress.CountedRows)
	require.Equal(t, int64(1), progress.DifferenceRows)
	require.NotNil(t, progress.PositiveDifferenceRows)
	require.Zero(t, *progress.PositiveDifferenceRows)
	require.Equal(t, "independent", progress.ReviewRelationship)
	lines = do(r, "GET", path+"/lines", "reader", "")
	var counts struct {
		Data []struct {
			CountedUnits    *int64 `json:"counted_units"`
			DifferenceUnits *int64 `json:"difference_units"`
			CountedBy       string `json:"counted_by"`
		}
	}
	require.NoError(t, json.Unmarshal(lines.Body.Bytes(), &counts))
	require.Len(t, counts.Data, 1)
	require.NotNil(t, counts.Data[0].CountedUnits)
	require.Equal(t, int64(8), *counts.Data[0].CountedUnits)
	require.NotNil(t, counts.Data[0].DifferenceUnits)
	require.Equal(t, int64(-2), *counts.Data[0].DifferenceUnits)
	require.Equal(t, "user-editor", counts.Data[0].CountedBy)
	newInput := strings.Replace(strings.Replace(input, `"expected_version":0`, `"expected_version":1`, 1), `"counted_units":8`, `"counted_units":9`, 1)
	edited := do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "reviewer", newInput)
	require.Equal(t, 200, edited.Code, edited.Body.String())
	participant := do(r, "GET", path, "reviewer", "")
	require.Equal(t, 200, participant.Code, participant.Body.String())
	var relationship struct {
		ReviewRelationship string `json:"review_relationship"`
	}
	require.NoError(t, json.Unmarshal(participant.Body.Bytes(), &relationship))
	require.Equal(t, "counter", relationship.ReviewRelationship)
	replayed := do(r, "PUT", command, "editor", input)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, counted.Body.String(), replayed.Body.String())
	recovered := do(r, "GET", command, "reader", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, counted.Body.String(), recovered.Body.String())
	require.Equal(t, 409, do(r, "PUT", command, "reviewer", input).Code)
	require.Equal(t, 409, do(r, "PUT", command, "editor", newInput).Code)
	require.Equal(t, 409, do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "editor", input).Code)
	// Accepted observations survive later movements; recording a new observation
	// against a changed book snapshot must fail even for net-zero movements.
	for i, quantity := range []int{1, -1} {
		moved := do(r, "PUT", outletPath("/stock/corrections/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "correction", fmt.Sprintf("stocktake:TEST-legacy-reference-%d", i), quantity))
		require.Equal(t, 200, moved.Code, moved.Body.String())
	}
	require.Equal(t, 409, do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(newInput, `"expected_version":1`, `"expected_version":2`, 1)).Code)
	creationReplay := do(r, "PUT", path, "editor", start)
	require.Equal(t, 200, creationReplay.Code, creationReplay.Body.String())
	require.JSONEq(t, created.Body.String(), creationReplay.Body.String())
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "PUT", command, token, input).Code)
	}
	require.Equal(t, 403, do(r, "PUT", command, "reader", input).Code)
}
