package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1386 B-3: discounts under the outlet ceiling.

// newDiscountWorld is a ready outlet where "manager" also holds the discount
// permission and "owner" records the ceiling.
func newDiscountWorld(t *testing.T) saleWorld {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "manager": testutil.TenantA, "owner": testutil.TenantA, "clerk": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"editor":     salePerms,
			"pharmacist": salePerms,
			"manager":    append(append([]authz.Permission{}, salePerms...), "pharmacy.sale.discount"),
			"owner":      {"pharmacy.catalogue.read", "pharmacy.access.manage", "pharmacy.sale.read", "pharmacy.discount.manage"},
			"clerk":      {"pharmacy.catalogue.read", "pharmacy.stock.read", "pharmacy.sale.read"},
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, s := range []string{"user-editor", "user-pharmacist", "user-manager", "user-owner", "user-clerk"} {
		grantCatalogue(t, r, s, "editor")
	}
	w := saleWorld{r: r, db: db, taxFrom: "2026-11-01", quarantine: uuid.Must(uuid.NewV7()).String(), sellable: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, do(r, "PUT", stockPath("locations/"+w.quarantine), "editor", `{"code":"TEST-Q","name":"TEST Quarantine"}`).Code)
	require.Equal(t, 200, do(r, "PUT", stockPath("locations/"+w.sellable), "editor", `{"code":"TEST-SHELF","name":"TEST Shelf","stock_status":"sellable"}`).Code)
	w.licence(t)
	w.gstin(t)
	w.profile(t, "TS")
	return w
}

func (w saleWorld) ceiling(t *testing.T, bps int) {
	t.Helper()
	res := do(w.r, "PUT", licencePath("/discount-ceiling/"+uuid.Must(uuid.NewV7()).String()), "owner", fmt.Sprintf(`{"max_bps":%d,"reason":"TEST owner decision"}`, bps))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
}

func discountSale(product string, units, bps int, amount int, reason string) string {
	r := ""
	if reason != "" {
		r = fmt.Sprintf(`,"discount_reason":%q`, reason)
	}
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":%d,"discount_bps":%d}],"tenders":[{"kind":"cash","amount_minor":%d,"tendered_minor":%d}]%s}`, product, units, bps, amount, amount, r)
}

func (w saleWorld) sellAs(t *testing.T, token, body string) (int, string) {
	t.Helper()
	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), token, body)
	return res.Code, res.Body.String()
}

type discountedLine struct {
	ListAmountMinor int64 `json:"list_amount_minor"`
	DiscountBps     int   `json:"discount_bps"`
	DiscountMinor   int64 `json:"discount_minor"`
	AmountMinor     int64 `json:"amount_minor"`
	TaxableMinor    int64 `json:"taxable_minor"`
	CGSTMinor       int64 `json:"cgst_minor"`
	SGSTMinor       int64 `json:"sgst_minor"`
}

func TestAHolderDiscountsWithinTheCeiling(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)

	got := do(w.r, "GET", licencePath("/discount-ceiling"), "clerk", "")
	require.Equal(t, http.StatusOK, got.Code)
	require.Contains(t, got.Body.String(), `"max_bps":1000`)

	// 10 units of a pack of 10 at 125.00, 10% off: 112.50, tax inside it.
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusCreated, code, out)
	var sale struct {
		TotalMinor     int64            `json:"total_minor"`
		DiscountMinor  int64            `json:"discount_minor"`
		DiscountReason string           `json:"discount_reason"`
		Lines          []discountedLine `json:"lines"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sale))
	require.EqualValues(t, 11250, sale.TotalMinor)
	require.EqualValues(t, 1250, sale.DiscountMinor)
	require.Equal(t, "TEST senior citizen", sale.DiscountReason)
	require.Len(t, sale.Lines, 1)
	l := sale.Lines[0]
	require.EqualValues(t, 12500, l.ListAmountMinor)
	require.Equal(t, 1000, l.DiscountBps)
	require.EqualValues(t, 1250, l.DiscountMinor)
	require.EqualValues(t, 11250, l.AmountMinor)
	amount, taxable, half := pharmacy.PriceLine(11250, 10, 10, "taxable", 120000)
	require.EqualValues(t, amount, l.AmountMinor, "GST is computed inside the discounted amount")
	require.EqualValues(t, taxable, l.TaxableMinor)
	require.EqualValues(t, half, l.CGSTMinor)

	code, out = w.sellAs(t, "manager", discountSale(p, 10, 1001, 11248, "TEST over"))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "is above this outlet's ceiling of 10%")
	code, out = w.sellAs(t, "manager", discountSale(p, 10, 0, 12500, ""))
	require.Equal(t, http.StatusCreated, code, "no discount needs no permission or reason: "+out)
}

func TestNoCeilingMeansNoDiscount(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 10)
	got := do(w.r, "GET", licencePath("/discount-ceiling"), "clerk", "")
	require.Equal(t, http.StatusOK, got.Code)
	require.JSONEq(t, `{"ceiling":null}`, got.Body.String())
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 100, 12375, "TEST loyalty"))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "ceiling of 0%")
}

func TestADiscountNeedsThePermissionAndAReason(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 2000)
	code, out := w.sellAs(t, "pharmacist", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusForbidden, code)
	require.Contains(t, out, "pharmacy.sale.discount")
	quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10,"discount_bps":1000}]}`, p))
	require.Equal(t, http.StatusForbidden, quote.Code, "a quote cannot show a discount its caller cannot give")
	code, _ = w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, ""))
	require.Equal(t, http.StatusBadRequest, code)
	code, _ = w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST"))
	require.Equal(t, http.StatusBadRequest, code, "a reason is at least 5 characters")
	code, _ = w.sellAs(t, "manager", discountSale(p, 10, 0, 12500, "TEST no discount here"))
	require.Equal(t, http.StatusBadRequest, code, "a reason without a discount is refused")
	code, _ = w.sellAs(t, "manager", discountSale(p, 10, 10001, 0, "TEST everything off"))
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", licencePath("/discount-ceiling/"+uuid.Must(uuid.NewV7()).String()), "manager", `{"max_bps":5000,"reason":"TEST self-raise"}`).Code,
		"a discount holder cannot raise its own ceiling")
}

func TestALoweredCeilingRefusesTheQuotedDiscount(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1500)
	quote := do(w.r, "POST", salePath("/quote"), "manager", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10,"discount_bps":1500}]}`, p))
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	require.Contains(t, quote.Body.String(), `"total_minor":10625`)
	w.ceiling(t, 500)
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1500, 10625, "TEST quoted earlier"))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "ceiling of 5%")
}

func TestASplitDiscountRoundsPerBatchLine(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2027-06-30", 999, 3)
	w.shelve(t, p, "2028-06-30", 999, 10)
	w.ceiling(t, 333)
	// 3 + 4 units of a pack of 10 at 9.99: 299 + 399 list; 3.33% off each,
	// rounded down per line: 9 + 13, so 290 + 386 = 676 charged.
	code, out := w.sellAs(t, "manager", discountSale(p, 7, 333, 676, "TEST staff purchase"))
	require.Equal(t, http.StatusCreated, code, out)
	var sale struct {
		TotalMinor    int64            `json:"total_minor"`
		DiscountMinor int64            `json:"discount_minor"`
		Lines         []discountedLine `json:"lines"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sale))
	require.Len(t, sale.Lines, 2)
	require.EqualValues(t, 299, sale.Lines[0].ListAmountMinor)
	require.EqualValues(t, 9, sale.Lines[0].DiscountMinor)
	require.EqualValues(t, 399, sale.Lines[1].ListAmountMinor)
	require.EqualValues(t, 13, sale.Lines[1].DiscountMinor)
	require.EqualValues(t, 676, sale.TotalMinor)
	require.EqualValues(t, 22, sale.DiscountMinor)
}

func TestADiscountedSaleReplaysAndItsDiscountIsItsIntent(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)
	id := uuid.Must(uuid.NewV7()).String()
	body := discountSale(p, 10, 1000, 11250, "TEST senior citizen")
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", salePath("/"+id), "manager", body).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", salePath("/"+id), "manager", body).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", salePath("/"+id), "manager", strings.Replace(body, "senior citizen", "staff", 1)).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", salePath("/"+id), "manager", discountSale(p, 10, 900, 11375, "TEST senior citizen")).Code)
}

// Review focus 4: the database alone holds a discount to the ceiling and
// its reason.
func TestTheDatabaseHoldsDiscountsToTheCeiling(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusCreated, code, out)
	var posted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &posted))
	forge := func(seq int, reason any, lineChange string) error {
		return catalogueTx(t, w.db, func(tx *gorm.DB) error {
			sale := uuid.Must(uuid.NewV7())
			if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
				seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject,discount_reason)
				SELECT ?,tenant_id,outlet_id,financial_year,?,?,sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
				currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor',? FROM pharmacy_sales WHERE id=?`,
				sale, seq, fmt.Sprintf("TS/2627/%d", seq), reason, posted.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
				schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,discount_bps,discount_minor)
				SELECT tenant_id,outlet_id,?,1,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,
				supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,`+lineChange+` FROM pharmacy_sale_lines WHERE sale_id=?`, sale, posted.ID).Error; err != nil {
				return err
			}
			// It is paid and its stock leaves the shelf, so only the discount is at fault.
			if err := tx.Exec(`INSERT INTO pharmacy_sale_tenders (tenant_id,outlet_id,sale_id,tender_number,kind,amount_minor,reference)
				SELECT tenant_id,outlet_id,?,1,'upi',total_minor,'TEST-FORGED' FROM pharmacy_sales WHERE id=?`, sale, sale).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_stock_movements (id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
				SELECT ?,tenant_id,outlet_id,location_id,batch_id,'sale',-quantity_units,'TEST forged',?,'user-editor' FROM pharmacy_sale_lines WHERE sale_id=?`,
				uuid.Must(uuid.NewV7()), "sale:"+sale.String()+":1", sale).Error
		})
	}
	same := "amount_minor,taxable_minor,cgst_minor,sgst_minor,discount_bps,discount_minor"
	require.ErrorContains(t, forge(97, "TEST forged", "amount_minor-1,taxable_minor-1,cgst_minor,sgst_minor,discount_bps,discount_minor+1"), "pharmacy_sale_line_discount_rounded",
		"a discount is the rounded percentage, never an arbitrary reduction")
	require.ErrorContains(t, forge(96, "TEST forged", "amount_minor-1250,taxable_minor-1250,cgst_minor,sgst_minor,2000,discount_minor+1250"), "within the outlet's ceiling")
	require.ErrorContains(t, forge(95, nil, same), "a discounted sale records its reason")
}

func TestDiscountArithmetic(t *testing.T) {
	for _, c := range []struct{ list, bps, discount, charged int64 }{
		{12500, 1000, 1250, 11250},
		{299, 333, 9, 290},  // 9.9567 rounds down
		{399, 333, 13, 386}, // 13.2867 rounds down
		{1, 9999, 0, 1},     // under one paisa of discount is none
		{12500, 10000, 12500, 0},
		{1000000000000000000, 10000, 1000000000000000000, 0}, // the schema's bounds pass int64 via big.Int
	} {
		d, a := pharmacy.DiscountLine(c.list, c.bps)
		require.Equal(t, []int64{c.discount, c.charged}, []int64{d, a}, "%+v", c)
	}
}

// Review minor 3: a ceiling lowered between the handler's check and the line
// insert is refused by the database, and the cashier hears it as a refusal.
func TestACeilingLoweredMidSaleIsARefusal(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)
	// The handler reads 10%; the guard then sees 0%, as if lowered in between.
	require.NoError(t, w.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_ceiling_lowered_mid_sale", SQL: `
		CREATE OR REPLACE FUNCTION pharmacy_sale_line_discount_guard() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		 IF NEW.discount_bps > 0 THEN
		  RAISE EXCEPTION 'a sale line''s discount is within the outlet''s ceiling' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_discount_ceiling';
		 END IF;
		 RETURN NEW;
		END $$;`}}))
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, `"not_sellable"`)
	require.Contains(t, out, "discount ceiling changed")
}
