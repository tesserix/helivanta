package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1371: NPPA ceiling prices, declared by one person and reviewed by another.

func pricePath(product, suffix string) string {
	return productPath(product) + "/price-control" + suffix
}

func ceilingBody(ceiling int64, effective string) string {
	return fmt.Sprintf(`{"price_control":"controlled","ceiling_minor":%d,"order_reference":"S.O. 1234(E)","order_date":"2026-10-01","effective_from":%q,"source":"TEST NPPA order"}`, ceiling, effective)
}

type priceState struct {
	State   string `json:"state"`
	Reason  string `json:"reason"`
	InForce *struct {
		ID           string `json:"id"`
		Controlled   bool   `json:"controlled"`
		CeilingMinor *int64 `json:"ceiling_minor"`
	} `json:"in_force"`
	History []struct {
		ID       string  `json:"id"`
		Decision *string `json:"decision"`
	} `json:"history"`
}

func (w saleWorld) priceState(t *testing.T, product string) priceState {
	t.Helper()
	res := do(w.r, "GET", pricePath(product, ""), "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var s priceState
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	return s
}

// declarePrice records a declaration by the editor and, unless decision is
// empty, has the pharmacist decide it.
func (w saleWorld) declarePrice(t *testing.T, product, body, decision string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", pricePath(product, "/"+id), "editor", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	if decision != "" {
		res = do(w.r, "POST", pricePath(product, "/"+id+"/review"), "pharmacist", `{"decision":"`+decision+`","reason":"checked against the gazette"}`)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	return id
}

func TestAPriceControlNeedsASecondPerson(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	s := w.priceState(t, product)
	require.Equal(t, "undeclared", s.State)
	require.Nil(t, s.InForce)

	id := uuid.Must(uuid.NewV7()).String()
	body := ceilingBody(1000, "2026-11-01")
	res := do(w.r, "PUT", pricePath(product, "/"+id), "editor", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.Equal(t, http.StatusOK, do(w.r, "PUT", pricePath(product, "/"+id), "editor", body).Code, "a retry replays")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", pricePath(product, "/"+id), "editor", ceilingBody(900, "2026-11-01")).Code, "the same id with another ceiling")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", pricePath(w.product(t, "TEST-ORS", "none"), "/"+id), "editor", body).Code, "the same id on another product")
	s = w.priceState(t, product)
	require.Equal(t, "blocked", s.State)
	require.Contains(t, s.Reason, "awaits review")

	res = do(w.r, "POST", pricePath(product, "/"+id+"/review"), "editor", `{"decision":"approved","reason":"mine"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "four_eyes")
	require.Equal(t, http.StatusForbidden, do(w.r, "POST", pricePath(product, "/"+id+"/review"), "clerk", `{"decision":"approved","reason":"x"}`).Code)
	res = do(w.r, "POST", pricePath(product, "/"+id+"/review"), "pharmacist", `{"decision":"approved","reason":"checked against the gazette"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, http.StatusConflict, do(w.r, "POST", pricePath(product, "/"+id+"/review"), "pharmacist", `{"decision":"rejected","reason":"again"}`).Code, "reviewed once")
	s = w.priceState(t, product)
	require.Equal(t, "in_force", s.State)
	require.Equal(t, id, s.InForce.ID)
	require.Equal(t, int64(1000), *s.InForce.CeilingMinor)

	for _, bad := range []string{
		`{"price_control":"controlled","effective_from":"2026-11-01","source":"TEST"}`,
		`{"price_control":"controlled","ceiling_minor":1000,"order_reference":"S.O. 1","order_date":"2026-11-02","effective_from":"2026-11-01","source":"TEST"}`,
		`{"price_control":"not_controlled","ceiling_minor":1000,"effective_from":"2026-11-01","source":"TEST"}`,
		`{"price_control":"not_controlled","effective_from":"2026-11-01","source":"  "}`,
		`{"price_control":"maybe","effective_from":"2026-11-01","source":"TEST"}`,
	} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", pricePath(product, "/"+uuid.Must(uuid.NewV7()).String()), "editor", bad).Code, bad)
	}
	require.Equal(t, http.StatusNotFound, do(w.r, "PUT", pricePath(uuid.Must(uuid.NewV7()).String(), "/"+uuid.Must(uuid.NewV7()).String()), "editor", body).Code)
}

func TestTheCeilingInForceFollowsItsDates(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	a := w.declarePrice(t, product, ceilingBody(1000, "2026-10-31"), "approved")
	w.declarePrice(t, product, ceilingBody(800, "2026-11-02"), "approved")
	s := w.priceState(t, product)
	require.Equal(t, "in_force", s.State)
	require.Equal(t, a, s.InForce.ID, "a change dated tomorrow leaves today's ceiling in force")

	c := w.declarePrice(t, product, ceilingBody(700, "2026-11-01"), "")
	require.Equal(t, "blocked", w.priceState(t, product).State, "a newer declaration awaiting review blocks the older approval")
	require.Equal(t, http.StatusOK, do(w.r, "POST", pricePath(product, "/"+c+"/review"), "pharmacist", `{"decision":"rejected","reason":"wrong order"}`).Code)
	s = w.priceState(t, product)
	require.Equal(t, "blocked", s.State, "a rejected latest declaration is corrected by a new one")
	require.Contains(t, s.Reason, "rejected")
	d := w.declarePrice(t, product, `{"price_control":"not_controlled","effective_from":"2026-11-01","source":"TEST not in the NLEM schedule"}`, "approved")
	s = w.priceState(t, product)
	require.Equal(t, d, s.InForce.ID)
	require.False(t, s.InForce.Controlled)
	require.Nil(t, s.InForce.CeilingMinor)

	// The latest start wins, not the latest entry: a backdated declaration
	// does not displace one that started after it.
	later := w.product(t, "TEST-ZINC", "none")
	started := w.declarePrice(t, later, ceilingBody(1000, "2026-10-31"), "approved")
	w.declarePrice(t, later, ceilingBody(1200, "2026-10-30"), "approved")
	require.Equal(t, started, w.priceState(t, later).InForce.ID)

	future := w.product(t, "TEST-ORS", "none")
	w.declarePrice(t, future, ceilingBody(500, "2026-12-01"), "approved")
	s = w.priceState(t, future)
	require.Equal(t, "blocked", s.State, "an approved ceiling not yet in effect leaves nothing in force")
	require.Contains(t, s.Reason, "taken effect")
}

type ceilingSale struct {
	CeilingChecked bool  `json:"nppa_ceiling_checked"`
	TotalMinor     int64 `json:"total_minor"`
	Lines          []struct {
		AmountMinor        int64   `json:"amount_minor"`
		TaxableMinor       int64   `json:"taxable_minor"`
		CGSTMinor          int64   `json:"cgst_minor"`
		ListAmountMinor    int64   `json:"list_amount_minor"`
		DiscountMinor      int64   `json:"discount_minor"`
		PriceLimit         string  `json:"price_limit"`
		CeilingMinor       *int64  `json:"ceiling_minor"`
		PriceDeclarationID *string `json:"price_declaration_id"`
	} `json:"lines"`
	ID string `json:"id"`
}

func (w saleWorld) sellCeiling(t *testing.T, body string) (int, ceilingSale, string) {
	t.Helper()
	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", body)
	var s ceilingSale
	if res.Code < 300 {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	}
	return res.Code, s, res.Body.String()
}

func TestTheCounterSellsWithinTheCeiling(t *testing.T) {
	w := newSaleWorld(t, true)
	// MRP ₹130 a pack of 10 at 12% GST: ₹13 a unit with tax, ₹11.60 before it.
	capped := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, capped, "2028-06-30", 13000, 20)
	declared := w.declarePrice(t, capped, ceilingBody(1000, "2026-11-01"), "approved")

	quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":3}]}`, capped))
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	require.Contains(t, quote.Body.String(), `"total_minor":3360`, "a ₹10 ceiling with 12% GST: ₹11.20 a unit")
	status, sale, body := w.sellCeiling(t, cashSale(capped, 3, 3360, 3360))
	require.Equal(t, http.StatusCreated, status, body)
	require.True(t, sale.CeilingChecked)
	line := sale.Lines[0]
	require.Equal(t, int64(3360), line.AmountMinor)
	require.Equal(t, int64(3000), line.TaxableMinor, "within ₹10 × 3 before GST")
	require.Equal(t, int64(180), line.CGSTMinor)
	require.Equal(t, "ceiling", line.PriceLimit)
	require.Equal(t, int64(1000), *line.CeilingMinor)
	require.Equal(t, declared, *line.PriceDeclarationID)
	read := do(w.r, "GET", salePath("/"+sale.ID), "pharmacist", "")
	require.Equal(t, http.StatusOK, read.Code)
	require.Contains(t, read.Body.String(), `"price_limit":"ceiling"`, "the invoice keeps which limit applied")
	require.Contains(t, read.Body.String(), `"nppa_ceiling_checked":true`)

	// A ceiling above the MRP leaves the MRP as the limit.
	loose := w.product(t, "TEST-ORS", "none")
	w.shelve(t, loose, "2028-06-30", 13000, 20)
	w.declarePrice(t, loose, ceilingBody(2000, "2026-11-01"), "approved")
	status, sale, body = w.sellCeiling(t, cashSale(loose, 3, 3900, 3900))
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "mrp", sale.Lines[0].PriceLimit)
	require.Equal(t, int64(2000), *sale.Lines[0].CeilingMinor)

	// Undeclared sells under its MRP alone, and the invoice says so.
	plain := w.product(t, "TEST-VITC", "none")
	w.shelve(t, plain, "2028-06-30", 13000, 20)
	status, sale, body = w.sellCeiling(t, cashSale(plain, 3, 3900, 3900))
	require.Equal(t, http.StatusCreated, status, body)
	require.False(t, sale.CeilingChecked)
	require.Nil(t, sale.Lines[0].PriceDeclarationID)
	require.Equal(t, "mrp", sale.Lines[0].PriceLimit)

	// Declared not controlled: checked, at MRP.
	w.declarePrice(t, plain, `{"price_control":"not_controlled","effective_from":"2026-11-01","source":"TEST not scheduled"}`, "approved")
	status, sale, body = w.sellCeiling(t, cashSale(plain, 3, 3900, 3900))
	require.Equal(t, http.StatusCreated, status, body)
	require.True(t, sale.CeilingChecked)
	require.Nil(t, sale.Lines[0].CeilingMinor)

	// A correction awaiting review stops the counter, with the reason.
	w.declarePrice(t, capped, ceilingBody(900, "2026-11-01"), "")
	status, _, body = w.sellCeiling(t, cashSale(capped, 3, 3024, 3024))
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "not_sellable")
	require.Contains(t, body, "awaits review")
	quote = do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":3}]}`, capped))
	require.Equal(t, http.StatusConflict, quote.Code, "nor is it quoted")
}

func TestADiscountOnACappedLineStaysWithinBoth(t *testing.T) {
	w := newDiscountWorld(t)
	w.ceiling(t, 2000)
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2028-06-30", 13000, 20)
	w.declarePrice(t, product, ceilingBody(1000, "2026-11-01"), "approved")
	status, body := w.sellAs(t, "manager", discountSale(product, 3, 1000, 3024, "TEST senior citizen"))
	require.Equal(t, http.StatusCreated, status, body)
	var sale ceilingSale
	require.NoError(t, json.Unmarshal([]byte(body), &sale))
	line := sale.Lines[0]
	require.Equal(t, int64(3360), line.ListAmountMinor, "the discount runs on the capped price")
	require.Equal(t, int64(336), line.DiscountMinor)
	require.Equal(t, int64(3024), line.AmountMinor)
	require.LessOrEqual(t, line.TaxableMinor, int64(3000))
	require.Equal(t, "ceiling", line.PriceLimit)
}

func TestTheCeilingHoldsInTheDatabase(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2028-06-30", 13000, 20)
	declared := w.declarePrice(t, product, ceilingBody(1000, "2026-11-01"), "approved")
	status, sale, body := w.sellCeiling(t, cashSale(product, 3, 3360, 3360))
	require.Equal(t, http.StatusCreated, status, body)
	copyLine := func(set string) error {
		return catalogueTx(t, w.db, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
				schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,
				price_declaration_id,ceiling_minor,price_limit)
				SELECT tenant_id,outlet_id,sale_id,2,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,
				supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,`+set+` FROM pharmacy_sale_lines WHERE sale_id=?`, sale.ID).Error
		})
	}
	require.ErrorContains(t, copyLine(`3472,3100,186,186,price_declaration_id,ceiling_minor,'ceiling'`), "above the NPPA ceiling")
	require.ErrorContains(t, copyLine(`amount_minor,taxable_minor,cgst_minor,sgst_minor,price_declaration_id,ceiling_minor+1,'ceiling'`), "names the price control in force")
	require.ErrorContains(t, copyLine(`amount_minor,taxable_minor,cgst_minor,sgst_minor,NULL,NULL,'mrp'`), "names the price control in force", "a declared product is not sold as undeclared")

	// The same ceiling under a declaration not in force is still refused.
	older := w.declarePrice(t, product, ceilingBody(1000, "2026-10-31"), "approved")
	require.ErrorContains(t, copyLine(`amount_minor,taxable_minor,cgst_minor,sgst_minor,'`+older+`'::uuid,ceiling_minor,'ceiling'`), "names the price control in force")

	w.declarePrice(t, product, ceilingBody(900, "2026-11-01"), "")
	require.ErrorContains(t, copyLine(`amount_minor,taxable_minor,cgst_minor,sgst_minor,price_declaration_id,ceiling_minor,'ceiling'`), "no ceiling in force")

	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_product_price_declarations SET ceiling_minor=5000 WHERE id=?`, declared).Error
	})
	require.Error(t, err, "a declaration is never changed")
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_price_declarations (id,tenant_id,outlet_id,product_id,controlled,effective_from,source,intent_sha256,declared_by)
			VALUES (?,?,?,?,true,'2026-11-01','TEST',repeat('a',64),'user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, product).Error
	})
	require.Error(t, err, "a controlled declaration carries its ceiling and order")
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_price_declarations (id,tenant_id,outlet_id,product_id,controlled,ceiling_minor,effective_from,source,intent_sha256,declared_by)
			VALUES (?,?,?,?,false,1000,'2026-11-01','TEST',repeat('a',64),'user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, product).Error
	})
	require.ErrorContains(t, err, "pharmacy_price_declaration_form", "an uncontrolled one carries none")
}

// The cap is the most a line can charge with its value before GST, as the
// counter splits it, within the ceiling.
func TestCeilingCapUnitsAndRounding(t *testing.T) {
	for _, rate := range []int64{0, 50000, 120000, 180000, 280000} {
		for _, ceiling := range []int64{1, 7, 99, 253, 1000, 4567, 99999} {
			for _, bps := range []int64{0, 1, 500, 3333} {
				capTotal := ceiling * 3
				nature := "taxable"
				if rate == 0 {
					nature = "nil_rated"
				}
				got, capped := pharmacy.WithinCeiling(1_000_000_000, capTotal, bps, nature, rate)
				require.True(t, capped)
				fits := func(a int64) bool {
					_, charged := pharmacy.DiscountLine(a, bps)
					_, before, _ := pharmacy.TaxInside(a, nature, rate)
					_, after, _ := pharmacy.TaxInside(charged, nature, rate)
					return before <= capTotal && after <= capTotal
				}
				require.True(t, fits(got), "rate %d ceiling %d bps %d: %d", rate, ceiling, bps, got)
				for a := got + 1; a <= got+20; a++ {
					require.False(t, fits(a), "rate %d ceiling %d bps %d: %d also fits above %d", rate, ceiling, bps, a, got)
				}
				same, again := pharmacy.WithinCeiling(got, capTotal, bps, nature, rate)
				require.Equal(t, got, same)
				require.False(t, again, "an amount already within the ceiling is the MRP's")
			}
		}
	}
}
