package pharmacy_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func TestStockRegisterReconcilesExactPeriodAndNeverAdvertisesUsableStock(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	root := strings.TrimSuffix(strings.Split(productPath(""), "/catalogue")[0], "/") + "/stock"
	var receipt, correction struct {
		CreatedAt time.Time `json:"created_at"`
	}
	body := stockBody(location, batch, "receipt", "TEST-REGISTER-RECEIPT", 25)
	path := root + "/movements/" + uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	require.Equal(t, 200, do(r, "PUT", path, "editor", body).Code)
	w = do(r, "PUT", root+"/corrections/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "correction", "TEST-REGISTER-DAMAGE", -4))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &correction))
	t.Logf("receipt=%s correction=%s wall=%s", receipt.CreatedAt, correction.CreatedAt, time.Now().UTC())
	window := "as_of=" + url.QueryEscape(correction.CreatedAt.Format(time.RFC3339Nano))
	w = do(r, "GET", root+"/positions?"+window, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Len(t, raw.Data, 1)
	row := raw.Data[0]
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "25", row["receipt_units"])
	require.Equal(t, "-4", row["correction_units"])
	require.Equal(t, location, row["location_id"])
	require.Equal(t, batch, row["batch_id"])
	require.Equal(t, "TEST-AMOX-500", row["sku"])
	require.Equal(t, "capsule", row["unit_of_issue"])
	require.Equal(t, "quarantine", row["stock_status"])
	require.Equal(t, float64(21), row["on_hand_units"])
	require.Equal(t, float64(0), row["dispensable_units"])
	from := url.QueryEscape(correction.CreatedAt.Format(time.RFC3339Nano))
	query := fmt.Sprintf("?location_id=%s&batch_id=%s&from=%s&%s", location, batch, from, window)
	w = do(r, "GET", root+"/reconciliation"+query, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var report struct {
		Location    string    `json:"location_id"`
		Batch       string    `json:"batch_id"`
		From        time.Time `json:"from"`
		AsOf        time.Time `json:"as_of"`
		Opening     int64     `json:"opening_units"`
		Receipts    string    `json:"receipt_units"`
		Corrections string    `json:"correction_units"`
		Closing     int64     `json:"closing_units"`
		Count       int64     `json:"movement_count"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	require.Equal(t, location, report.Location)
	require.Equal(t, batch, report.Batch)
	require.True(t, correction.CreatedAt.Equal(report.From), report.From)
	require.True(t, correction.CreatedAt.Equal(report.AsOf), report.AsOf)
	require.EqualValues(t, 25, report.Opening)
	require.Equal(t, "0", report.Receipts)
	require.Equal(t, "-4", report.Corrections)
	require.EqualValues(t, 21, report.Closing)
	require.EqualValues(t, 1, report.Count)
	require.Equal(t, report.Opening-4, report.Closing)
	w = do(r, "GET", root+fmt.Sprintf("/reconciliation?location_id=%s&batch_id=%s&%s", location, batch, window), "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	require.True(t, time.Unix(0, 0).Equal(report.From), report.From)
	require.EqualValues(t, 0, report.Opening)
	require.EqualValues(t, 21, report.Closing)
	w = do(r, "GET", root+"/movements"+query, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Len(t, raw.Data, 1)
	require.Equal(t, "TEST-REGISTER-DAMAGE", raw.Data[0]["source_reference"])
}

func TestStockRegisterRejectsInvalidForeignAndRevokedScopes(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	root := strings.TrimSuffix(strings.Split(productPath(""), "/catalogue")[0], "/") + "/stock"
	// Scope checks must not depend on host/container clock synchronization.
	cutoff := url.QueryEscape("2020-01-01T00:00:00Z")
	for _, suffix := range []string{"/positions?as_of=" + cutoff, "/reconciliation?location_id=" + location + "&batch_id=" + batch + "&as_of=" + cutoff} {
		require.Equal(t, 403, do(r, "GET", root+suffix, "denied", "").Code)
		for _, token := range []string{"sibling", "foreign"} {
			require.Equal(t, 404, do(r, "GET", root+suffix, token, "").Code)
		}
		require.Equal(t, 401, do(r, "GET", root+suffix, "unknown", "").Code)
	}
	for _, query := range []string{"as_of=bad", "as_of=2020-01-01T00%3A00%3A00.000000001Z", "as_of=2099-01-01T00%3A00%3A00Z", "", "as_of=" + cutoff + "&from=2099-01-01T00%3A00%3A00Z", "as_of=" + cutoff + "&expiry=unknown", "as_of=" + cutoff + "&location_id=no"} {
		require.Equal(t, 400, do(r, "GET", root+"/positions?"+query, "editor", "").Code, query)
	}
	missingLocation := do(r, "GET", root+"/positions?as_of="+cutoff+"&location_id="+uuid.Must(uuid.NewV7()).String(), "editor", "")
	require.Equal(t, 404, missingLocation.Code, missingLocation.Body.String())
	require.Equal(t, 404, do(r, "GET", root+"/reconciliation?as_of="+cutoff+"&location_id="+location+"&batch_id="+uuid.Must(uuid.NewV7()).String(), "editor", "").Code)
	grantCatalogue(t, r, "user-editor", "reader")
	require.Equal(t, 200, do(r, "GET", root+"/positions?as_of="+cutoff, "editor", "").Code)
	require.Equal(t, 200, do(r, "DELETE", strings.TrimSuffix(root, "/stock")+"/access/user-editor", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", root+"/positions?as_of="+cutoff, "editor", "").Code)
}

func TestStockRegisterPaginatesZeroPositionsAndUsesOutletLocalExpiry(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, location, _ := stockFixture(t, r)
	root := strings.TrimSuffix(strings.Split(productPath(""), "/catalogue")[0], "/") + "/stock"
	location2 := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", root+"/locations/"+location2, "editor", `{"code":"TEST-SECOND","name":"TEST Second quarantine"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	// At this UTC time the India outlet is already on October 2.
	cutoff := "2026-10-01T19:00:00Z"
	for i, expiry := range []string{"2026-10-01", "2026-10-02"} {
		batch := uuid.Must(uuid.NewV7()).String()
		w = do(r, "PUT", root+"/batches/"+batch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":%q,"expires_on":%q,"mrp_minor":12500}`, product, fmt.Sprintf("TEST-LOCAL-%d", i), expiry))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','user-editor',true),set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
				return err
			}
			for j, loc := range []string{location, location2} {
				for k, qty := range []int{5, -5} {
					kind := "receipt"
					if k == 1 {
						kind = "correction"
					}
					if err := tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at) VALUES (?,?,?,?,?,?,?,'TEST deterministic historical register',?,'user-editor',?::timestamptz)`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, loc, batch, kind, qty, fmt.Sprintf("TEST-LOCAL-%d-%d-%d", i, j, k), cutoff).Error; err != nil {
						return err
					}
				}
			}
			return nil
		}))
	}
	cursor := ""
	w = do(r, "GET", root+"/positions?as_of="+url.QueryEscape(cutoff)+"&balance=on_hand", "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var positive struct{ Data []json.RawMessage }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &positive))
	require.Empty(t, positive.Data)
	seen := map[string]bool{}
	for {
		w = do(r, "GET", root+"/positions?limit=1&as_of="+url.QueryEscape(cutoff)+"&cursor="+url.QueryEscape(cursor), "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var result struct {
			Data []struct {
				ID      string
				OnHand  int64  `json:"on_hand_units"`
				Expiry  string `json:"expiry_status"`
				Expires string `json:"expires_on"`
			}
			Page struct {
				HasMore bool   `json:"has_more"`
				Next    string `json:"next_cursor"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Data, 1)
		row := result.Data[0]
		require.False(t, seen[row.ID])
		seen[row.ID] = true
		require.Zero(t, row.OnHand)
		if row.Expires == "2026-10-01" {
			require.Equal(t, "expired", row.Expiry)
		} else {
			require.Equal(t, "unexpired", row.Expiry)
		}
		if !result.Page.HasMore {
			break
		}
		cursor = result.Page.Next
		require.NotEmpty(t, cursor)
	}
	require.Len(t, seen, 4)
	for _, filter := range []string{"expired", "unexpired"} {
		w = do(r, "GET", root+"/positions?as_of="+url.QueryEscape(cutoff)+"&expiry="+filter+"&location_id="+location+"&product_id="+product, "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var result struct {
			Data []struct {
				Expiry string `json:"expiry_status"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Data, 1)
		require.Equal(t, filter, result.Data[0].Expiry)
	}
}

// #1303 E3: a stock position is a lot at a location, and a reload reopens
// it by both, with no other filter.
func TestStockRegisterFiltersOnePositionByLot(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, location, batch := stockFixture(t, r)
	root := strings.TrimSuffix(strings.Split(productPath(""), "/catalogue")[0], "/") + "/stock"
	other := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", root+"/batches/"+other, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT-OTHER","expires_on":"2031-01-31","mrp_minor":12500}`, product))
	require.Equal(t, 200, w.Code, w.Body.String())
	var last struct {
		CreatedAt time.Time `json:"created_at"`
	}
	for _, lot := range []string{batch, other} {
		w = do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, lot, "receipt", "TEST-LOT-FILTER-"+lot[len(lot)-4:], 3))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &last))
	}
	window := "as_of=" + url.QueryEscape(last.CreatedAt.Format(time.RFC3339Nano))
	var both, one struct {
		Data []map[string]any `json:"data"`
	}
	w = do(r, "GET", root+"/positions?"+window, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &both))
	require.Len(t, both.Data, 2)

	w = do(r, "GET", root+"/positions?"+window+"&batch_id="+batch+"&location_id="+location, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &one))
	require.Len(t, one.Data, 1)
	require.Equal(t, batch, one.Data[0]["batch_id"])
	require.Equal(t, location, one.Data[0]["location_id"])
	require.Equal(t, "TEST-LOT", one.Data[0]["lot_number"])

	// Unknown to this outlet is the same answer as an unknown location.
	missing := do(r, "GET", root+"/positions?"+window+"&batch_id="+uuid.Must(uuid.NewV7()).String(), "editor", "")
	require.Equal(t, 404, missing.Code, missing.Body.String())
	require.Equal(t, 400, do(r, "GET", root+"/positions?"+window+"&batch_id=no", "editor", "").Code)
	// The export answers for the same rows as the list.
	w = do(r, "GET", root+"/positions.csv?"+window+"&batch_id="+batch, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "TEST-LOT")
	require.NotContains(t, w.Body.String(), "TEST-LOT-OTHER")
}

// #1638: the reconciliation's kind totals must cover every kind the database allows.
func TestStockMovementKindsMatchTheDatabaseConstraint(t *testing.T) {
	_, db := catalogueHarness(t)
	var def string
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='pharmacy_stock_movements_kind_check'`).Scan(&def).Error
	}))
	var kinds []string
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(def, -1) {
		kinds = append(kinds, m[1])
	}
	require.NotEmpty(t, kinds, def)
	require.ElementsMatch(t, kinds, pharmacy.StockMovementKinds)
}

// footing reads a reconciliation and asserts opening + every kind = closing.
func footing(t *testing.T, w saleWorld, location, batch string, from, asOf time.Time) map[string]any {
	t.Helper()
	res := do(w.r, "GET", stockPath(fmt.Sprintf("reconciliation?location_id=%s&batch_id=%s&from=%s&as_of=%s", location, batch,
		url.QueryEscape(from.Format(time.RFC3339Nano)), url.QueryEscape(asOf.Format(time.RFC3339Nano)))), "editor", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &got))
	sum := int64(got["opening_units"].(float64))
	for _, kind := range pharmacy.StockMovementKinds {
		v, ok := got[kind+"_units"].(string)
		require.True(t, ok, "missing %s_units in %v", kind, got)
		n, err := strconv.ParseInt(v, 10, 64)
		require.NoError(t, err)
		sum += n
	}
	require.EqualValues(t, got["closing_units"], sum, "opening + the parts = closing: %v", got)
	return got
}

// everyKindLot is one lot received, corrected, released, sold and returned;
// released and last bound the movements after the receipt and correction.
func everyKindLot(t *testing.T) (w saleWorld, batch string, released, last time.Time) {
	t.Helper()
	w = newSaleWorld(t, true)
	p := w.product(t, "TEST-RECON-1", "none")
	batch = receive(t, w.r, p, w.quarantine, "2028-06-30", `,"mrp_minor":12500`)
	require.Equal(t, 200, do(w.r, "PUT", stockPath("corrections/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(w.quarantine, batch, "correction", "TEST-RECON-DAMAGE", -3)).Code)
	res := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":30,"reason":"TEST shelved"}`, batch, w.quarantine, w.sellable))
	require.Equal(t, 200, res.Code, res.Body.String())
	sale := w.soldSale(t, p, 10, 12500)
	code, _, out := w.returnUnits(t, returnBody(sale, 4, w.quarantine, 5000))
	require.Equal(t, 201, code, out)
	// A supply to a hospital order needs a supplier command and allocation behind it
	// (pharmacy_supplier_dispatch_guard), so it is not recorded here; its column is
	// still required by footing and pinned to the constraint by the test above.
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT min(created_at) FROM pharmacy_stock_movements WHERE batch_id=? AND kind='release'`, batch).Scan(&released).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT max(created_at) FROM pharmacy_stock_movements WHERE batch_id=?`, batch).Scan(&last).Error
	}))

	return w, batch, released, last
}

func TestStockReconciliationFootsEveryMovementKind(t *testing.T) {
	w, batch, released, last := everyKindLot(t)
	q := footing(t, w, w.quarantine, batch, released, last)
	require.EqualValues(t, 97, q["opening_units"])
	for kind, want := range map[string]string{"receipt": "0", "correction": "0", "release": "-30", "sale": "0", "return": "4", "supplier_dispatch": "0"} {
		require.Equal(t, want, q[kind+"_units"], kind)
	}
	require.EqualValues(t, 71, q["closing_units"])
	s := footing(t, w, w.sellable, batch, released, last)
	require.EqualValues(t, 0, s["opening_units"])
	for kind, want := range map[string]string{"receipt": "0", "correction": "0", "release": "30", "sale": "-10", "return": "0", "supplier_dispatch": "0"} {
		require.Equal(t, want, s[kind+"_units"], kind)
	}
	require.EqualValues(t, 20, s["closing_units"])
}

func TestStockPositionsAndExportFootEveryMovementKind(t *testing.T) {
	w, batch, _, last := everyKindLot(t)
	asOf := url.QueryEscape(last.Format(time.RFC3339Nano))
	res := do(w.r, "GET", stockPath("positions?as_of="+asOf+"&batch_id="+batch), "editor", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	var page struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	want := map[string]map[string]string{
		w.quarantine: {"receipt": "100", "correction": "-3", "release": "-30", "sale": "0", "return": "4", "supplier_dispatch": "0"},
		w.sellable:   {"receipt": "0", "correction": "0", "release": "30", "sale": "-10", "return": "0", "supplier_dispatch": "0"},
	}
	for _, row := range page.Data {
		var sum int64
		for _, kind := range pharmacy.StockMovementKinds {
			v, ok := row[kind+"_units"].(string)
			require.True(t, ok, "missing %s_units in %v", kind, row)
			require.Equal(t, want[row["location_id"].(string)][kind], v, kind)
			n, err := strconv.ParseInt(v, 10, 64)
			require.NoError(t, err)
			sum += n
		}
		require.EqualValues(t, row["on_hand_units"], sum, "on hand = the parts: %v", row)
	}
	res = do(w.r, "GET", stockPath("positions.csv?as_of="+asOf), "editor", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	rows, err := csv.NewReader(strings.NewReader(res.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, []string{"on_hand_units", "receipt_units", "correction_units", "dispensable_units", "release_units", "sale_units", "return_units", "supplier_dispatch_units"}, rows[0][len(rows[0])-8:])
	for _, row := range rows[1:] {
		values := map[string]string{}
		for i, name := range rows[0] {
			values[name] = row[i]
		}
		var sum int64
		for _, kind := range pharmacy.StockMovementKinds {
			require.Equal(t, want[values["location_id"]][kind], values[kind+"_units"], kind)
			n, _ := strconv.ParseInt(values[kind+"_units"], 10, 64)
			sum += n
		}
		require.Equal(t, values["on_hand_units"], strconv.FormatInt(sum, 10))
	}
}
