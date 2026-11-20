package pharmacy_test

import (
	"context"

	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func holdBody(w releaseWorld, quantity int) string {
	return fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"quarantine_location_id":%q,"quantity_units":%d,"reason":"TEST physically set aside","physical_disposition":"moved_to_quarantine"}`, w.batch, w.sellable, w.quarantine, quantity)
}

func TestStockHoldConservesStockAndRecoversOriginalActor(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w := newReleaseWorldForOwnership(t, ownership)
			status, body := w.release(w.batch, 30)
			require.Equal(t, 200, status, body)
			id := uuid.Must(uuid.NewV7()).String()
			path := stockPath("holds/" + id)
			input := holdBody(w, 8)
			held := do(w.r, "PUT", path, "pharmacist", input)
			require.Equal(t, 200, held.Code, held.Body.String())
			var receipt pharmacy.StockHoldReceipt
			require.NoError(t, json.Unmarshal(held.Body.Bytes(), &receipt))
			require.Equal(t, id, receipt.ID.String())
			require.Equal(t, uuid.MustParse(w.batch), receipt.BatchID)
			require.Equal(t, uuid.MustParse(w.sellable), receipt.FromLocationID)
			require.Equal(t, uuid.MustParse(w.quarantine), receipt.QuarantineLocationID)
			require.EqualValues(t, 8, *receipt.QuantityUnits)
			require.Equal(t, "user-pharmacist", receipt.HeldBy)
			require.NotEqual(t, receipt.OutMovementID, receipt.InMovementID)
			require.Equal(t, uuid.Version(7), receipt.OutMovementID.Version())
			require.Equal(t, uuid.Version(7), receipt.InMovementID.Version())
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				var healthy bool
				if err := tx.Raw("SELECT pharmacy_operating_choice_integrity() AND pharmacy_operating_choice_support()").Scan(&healthy).Error; err != nil {
					return err
				}
				require.True(t, healthy, "hold tables must preserve native Staff location-choice authority")
				var count int64
				if err := tx.Table("pharmacy_stock_holds").Where("id=?", id).Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, count)
				if err := tx.Exec("SELECT set_config('app.pharmacy_operating_choices','TEST-choice-purpose',true)").Error; err != nil {
					return err
				}
				if err := tx.Table("pharmacy_stock_holds").Where("id=?", id).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count, "a location-choice purpose must never disclose physical hold evidence")
				return nil
			}))

			_, err := time.Parse(time.RFC3339Nano, receipt.HeldAt)
			require.NoError(t, err)
			require.EqualValues(t, 22, w.balance(t, w.sellable, w.batch))
			require.EqualValues(t, 78, w.balance(t, w.quarantine, w.batch))
			replay := do(w.r, "PUT", path, "pharmacist", input)
			require.Equal(t, 200, replay.Code, replay.Body.String())
			require.JSONEq(t, held.Body.String(), replay.Body.String())
			recovered := do(w.r, "GET", path, "pharmacist", "")
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, held.Body.String(), recovered.Body.String())
			require.Equal(t, 404, do(w.r, "GET", path, "editor", "").Code)
			require.Equal(t, 409, do(w.r, "PUT", path, "editor", input).Code)
			require.Equal(t, 409, do(w.r, "PUT", path, "pharmacist", holdBody(w, 7)).Code)
			require.Equal(t, 403, do(w.r, "PUT", path, "clerk", input).Code)
			for _, actor := range []string{"foreign", "sibling"} {
				require.Equal(t, 404, do(w.r, "PUT", path, actor, input).Code)
				require.Equal(t, 404, do(w.r, "GET", path, actor, "").Code)
			}
			excess := stockPath("holds/" + uuid.Must(uuid.NewV7()).String())
			failed := do(w.r, "PUT", excess, "pharmacist", holdBody(w, 23))
			require.Equal(t, 409, failed.Code, failed.Body.String())
			require.Equal(t, 404, do(w.r, "GET", excess, "pharmacist", "").Code)
			require.EqualValues(t, 22, w.balance(t, w.sellable, w.batch))
			require.EqualValues(t, 78, w.balance(t, w.quarantine, w.batch))
			asOf := url.QueryEscape(time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
			exported := do(w.r, "GET", stockPath("positions.csv?as_of="+asOf), "editor", "")
			require.Equal(t, 200, exported.Code, exported.Body.String())
			records, err := csv.NewReader(strings.NewReader(exported.Body.String())).ReadAll()
			require.NoError(t, err)
			require.Len(t, records, 3)
			col := map[string]int{}
			for i, name := range records[0] {
				col[name] = i
			}
			require.Contains(t, col, "quarantine_hold_units")
			for _, row := range records[1:] {
				expected := "8"
				if row[col["location_id"]] == w.sellable {
					expected = "-8"
				}
				require.Equal(t, expected, row[col["quarantine_hold_units"]])
			}
		})
	}
}

func TestStockHoldEvidenceFailureRollsBackBothLegs(t *testing.T) {
	w := newReleaseWorld(t)
	status, body := w.release(w.batch, 30)
	require.Equal(t, 200, status, body)
	id := uuid.Must(uuid.NewV7()).String()
	path := stockPath("holds/" + id)
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_stock_holds'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.stock_movement_recorded.v1'"},
	} {
		apply := func(sql string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_hold_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() {
			apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_hold_evidence_failure")
		}
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_hold_evidence_failure CHECK(" + fault.check + ") NOT VALID")
		failed := do(w.r, "PUT", path, "pharmacist", holdBody(w, 8))
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "synthetic_hold_evidence_failure")
		require.Equal(t, 404, do(w.r, "GET", path, "pharmacist", "").Code)
		require.EqualValues(t, 30, w.balance(t, w.sellable, w.batch))
		require.EqualValues(t, 70, w.balance(t, w.quarantine, w.batch))
		clear()
	}
	held := do(w.r, "PUT", path, "pharmacist", holdBody(w, 8))
	require.Equal(t, 200, held.Code, held.Body.String())
	require.EqualValues(t, 22, w.balance(t, w.sellable, w.batch))
	require.EqualValues(t, 78, w.balance(t, w.quarantine, w.batch))
}

func TestStockHoldDatabaseRejectsIncompleteAndForgedPairs(t *testing.T) {
	w := newReleaseWorld(t)
	status, body := w.release(w.batch, 30)
	require.Equal(t, 200, status, body)
	id := uuid.Must(uuid.NewV7()).String()
	out, in := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	insert := func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_holds(id,tenant_id,outlet_id,batch_id,from_location_id,quarantine_location_id,quantity_units,reason,physical_disposition,out_movement_id,in_movement_id,intent_sha256,actor_subject)
   VALUES(?,current_setting('app.tenant_id')::uuid,?,?,?,?,1,'TEST proof','moved_to_quarantine',?,?,?,'user-editor')`, id, pharmaOutlet, w.batch, w.sellable, w.quarantine, out, in, strings.Repeat("a", 64)).Error
	}
	err := catalogueTx(t, w.db, insert)
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg)
	require.True(t, pg.Code == "23503" || pg.Code == "23514", pg.Message)
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := insert(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
   VALUES(?,current_setting('app.tenant_id')::uuid,?,?,?,'quarantine_hold',-2,'TEST proof',?,'user-editor')`, out, pharmaOutlet, w.sellable, w.batch, "hold:"+id+":out").Error
	})
	require.ErrorContains(t, err, "hold movements require exact immutable evidence")
	require.EqualValues(t, 30, w.balance(t, w.sellable, w.batch))
	require.EqualValues(t, 70, w.balance(t, w.quarantine, w.batch))
}
