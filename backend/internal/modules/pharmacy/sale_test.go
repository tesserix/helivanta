package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1386 B-1: an OTC counter sale.

var salePerms = append(append([]authz.Permission{}, releasePerms...), "pharmacy.licence.manage", "pharmacy.licence.review", "pharmacy.sale.create", "pharmacy.sale.read", "pharmacy.prescription.record", "pharmacy.prescription.read", "pharmacy.sale.return")

type saleWorld struct {
	r                    *gin.Engine
	db                   *tenantdb.DB
	quarantine, sellable string
	taxFrom              string
	cloud                *testinfra.GCSProtocol
	files                *attachments.GCS
	bus                  *events.Bus
}

// newSaleWorld is an outlet on 1 Nov 2026 (IST) with a quarantine and a
// sellable location. ready adds what a sale needs of the outlet.
func newSaleWorld(t *testing.T, ready bool) saleWorld {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "clerk": testutil.TenantA, "owner": testutil.TenantA, "stocker": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     salePerms,
			"pharmacist": salePerms,
			"clerk":      {"pharmacy.catalogue.read", "pharmacy.stock.read", "pharmacy.sale.read"},
			"stocker":    {"pharmacy.catalogue.read", "pharmacy.stock.read"},
			// The owner's sale and catalogue reads, without prescriptions.
			"owner":   {"pharmacy.catalogue.read", "pharmacy.sale.read", "pharmacy.tax.manage", "pharmacy.discount.manage"},
			"foreign": salePerms,
		}, Modules: []platform.Module{pharmacy.New()}, Documents: files, PharmacyEvidenceEnabled: true,
	})
	seedPharmacyOutlet(t, r)
	for _, s := range []string{"user-editor", "user-pharmacist", "user-clerk", "user-owner", "user-stocker"} {
		grantCatalogue(t, r, s, "editor")
	}
	w := saleWorld{r: r, db: db, cloud: cloud, files: files, bus: bus, taxFrom: "2026-11-01", quarantine: uuid.Must(uuid.NewV7()).String(), sellable: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, do(r, "PUT", stockPath("locations/"+w.quarantine), "editor", `{"code":"TEST-Q","name":"TEST Quarantine"}`).Code)
	require.Equal(t, 200, do(r, "PUT", stockPath("locations/"+w.sellable), "editor", `{"code":"TEST-SHELF","name":"TEST Shelf","stock_status":"sellable"}`).Code)
	if ready {
		w.licence(t)
		w.gstin(t)
		w.profile(t, "TS")
	}
	return w
}

func (w saleWorld) licence(t *testing.T) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2026-01-01", "2030-12-31")).Code)
	require.Equal(t, http.StatusOK, do(w.r, "POST", licencePath("/licences/"+id+"/review"), "pharmacist", `{"decision":"approved","reason":"checked the original"}`).Code)
}

func (w saleWorld) gstin(t *testing.T) {
	t.Helper()
	res := do(w.r, "POST", registrationsPath(pharmaOutlet), "editor", registrationBody("27", "27PQRST5678K2Z1", "2026-11-01", ""))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
}

func (w saleWorld) profile(t *testing.T, prefix string) {
	t.Helper()
	res := do(w.r, "PUT", licencePath("/invoice-profile/"+uuid.Must(uuid.NewV7()).String()), "editor",
		fmt.Sprintf(`{"legal_name":"TEST Main Pharmacy LLP","address":"TEST 1 Market Road, Pune 411001","invoice_prefix":%q}`, prefix))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
}

// product makes an active product of the given class, taxed at 12%.
func (w saleWorld) product(t *testing.T, sku, class string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(productBody(0, "TEST "+sku), `"TEST-AMOX-500"`, fmt.Sprintf("%q", sku), 1)
	res := do(w.r, "PUT", productPath(id), "editor", body)
	require.Equal(t, 200, res.Code, res.Body.String())
	res = do(w.r, "POST", productPath(id)+"/tax-classifications", "editor", classificationBody("30049099", "taxable", "12", "", w.taxFrom, ""))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	approveClass(t, w.r, id, class, false)
	return id
}

// shelve receives 100 units of a batch and releases some of them to sale.
func (w saleWorld) shelve(t *testing.T, product, expires string, mrp, units int) string {
	t.Helper()
	batch := receive(t, w.r, product, w.quarantine, expires, fmt.Sprintf(`,"mrp_minor":%d`, mrp))
	res := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":%d,"reason":"TEST shelved"}`, batch, w.quarantine, w.sellable, units))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	return batch
}

func salePath(suffix string) string { return licencePath("/sales" + suffix) }

func cashSale(product string, units, amount, tendered int) string {
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":%d}],"tenders":[{"kind":"cash","amount_minor":%d,"tendered_minor":%d}]}`, product, units, amount, tendered)
}

type saleResp struct {
	InvoiceNumber string `json:"invoice_number"`
	SellerGSTIN   string `json:"seller_gstin"`
	PlaceOfSupply string `json:"place_of_supply"`
	TotalMinor    int64  `json:"total_minor"`
	TaxableMinor  int64  `json:"taxable_minor"`
	CGSTMinor     int64  `json:"cgst_minor"`
	SGSTMinor     int64  `json:"sgst_minor"`
	ClassOrigin   string `json:"class_origin"`
	Lines         []struct {
		BatchID       string `json:"batch_id"`
		QuantityUnits int64  `json:"quantity_units"`
		AmountMinor   int64  `json:"amount_minor"`
		TaxableMinor  int64  `json:"taxable_minor"`
		CGSTMinor     int64  `json:"cgst_minor"`
	} `json:"lines"`
	Tenders []struct {
		ChangeMinor *int64 `json:"change_minor"`
	} `json:"tenders"`
}

func (w saleWorld) sell(t *testing.T, body string) (int, saleResp, string) {
	t.Helper()
	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", body)
	var s saleResp
	if res.Code < 300 {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	}
	return res.Code, s, res.Body.String()
}

func (w saleWorld) available(t *testing.T, product string) (int64, *string) {
	t.Helper()
	res := do(w.r, "GET", salePath("/availability?product_id="+product), "clerk", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	var a struct {
		Units   int64   `json:"sellable_units"`
		Nearest *string `json:"nearest_expiry"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &a))
	return a.Units, a.Nearest
}

func TestASalePricesAtBatchMRPEarliestExpiryFirst(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	later := w.shelve(t, product, "2028-06-30", 13000, 100)
	earlier := w.shelve(t, product, "2027-03-31", 12500, 5)
	units, nearest := w.available(t, product)
	require.Equal(t, int64(105), units)
	require.Equal(t, "2027-03-31", *nearest)

	quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":8}]}`, product))
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	var q saleResp
	require.NoError(t, json.Unmarshal(quote.Body.Bytes(), &q))
	require.Equal(t, []int64{10150, 9062, 544}, []int64{q.TotalMinor, q.TaxableMinor, q.CGSTMinor}, "a quote prices as the sale will")
	require.Len(t, q.Lines, 2)
	units, _ = w.available(t, product)
	require.Equal(t, int64(105), units, "a quote moves no stock")
	require.Equal(t, http.StatusForbidden, do(w.r, "POST", salePath("/quote"), "clerk", `{"lines":[]}`).Code)

	id := uuid.Must(uuid.NewV7()).String()
	body := cashSale(product, 8, 10150, 20000)
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var s saleResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	require.Equal(t, "TS/2627/1", s.InvoiceNumber)
	require.Equal(t, "27PQRST5678K2Z1", s.SellerGSTIN)
	require.Equal(t, "27", s.PlaceOfSupply)
	require.Equal(t, "declared_by_outlet", s.ClassOrigin)
	// 5 of the earlier batch at 12500 per 10, then 3 of the later at 13000.
	require.Len(t, s.Lines, 2)
	require.Equal(t, earlier, s.Lines[0].BatchID)
	require.Equal(t, []int64{5, 6250, 5580, 335}, []int64{s.Lines[0].QuantityUnits, s.Lines[0].AmountMinor, s.Lines[0].TaxableMinor, s.Lines[0].CGSTMinor})
	require.Equal(t, later, s.Lines[1].BatchID)
	require.Equal(t, []int64{3, 3900, 3482, 209}, []int64{s.Lines[1].QuantityUnits, s.Lines[1].AmountMinor, s.Lines[1].TaxableMinor, s.Lines[1].CGSTMinor})
	require.Equal(t, []int64{10150, 9062, 544, 544}, []int64{s.TotalMinor, s.TaxableMinor, s.CGSTMinor, s.SGSTMinor})
	require.Equal(t, int64(9850), *s.Tenders[0].ChangeMinor)

	rel := releaseWorld{r: w.r}
	require.Equal(t, int64(0), rel.balance(t, w.sellable, earlier))
	require.Equal(t, int64(97), rel.balance(t, w.sellable, later))

	again := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, again.Code, "replay returns the posted invoice")
	require.Contains(t, again.Body.String(), `"invoice_number":"TS/2627/1"`)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", salePath("/"+id), "pharmacist", cashSale(product, 1, 1300, 1300)).Code)
	require.Equal(t, int64(97), rel.balance(t, w.sellable, later), "a replay moves nothing")

	code, next, _ := w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"upi","amount_minor":1300,"reference":"TEST-UPI-1"}]}`, product))
	require.Equal(t, http.StatusCreated, code)
	require.Equal(t, "TS/2627/2", next.InvoiceNumber)

	list := do(w.r, "GET", salePath(""), "clerk", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), "TS/2627/2")
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", salePath("/"+id), "foreign", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "clerk", body).Code)
}

func TestASaleIsRefusedWithItsReasonAndUsesNoNumber(t *testing.T) {
	w := newSaleWorld(t, false)
	otc := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, otc, "2028-06-30", 13000, 10)
	body := cashSale(otc, 1, 1300, 1300)
	expect := func(reason string) {
		t.Helper()
		code, _, out := w.sell(t, body)
		require.Equal(t, http.StatusConflict, code, out)
		require.Contains(t, out, `"not_sellable"`)
		require.Contains(t, out, reason)
	}
	expect("no reviewed retail drug licence valid today")
	w.licence(t)
	expect("no GSTIN in force today")
	w.gstin(t)
	expect("no invoice details recorded")
	w.profile(t, "TS")

	h := w.product(t, "TEST-AMOX-500", "H")
	w.shelve(t, h, "2028-06-30", 12500, 10)
	body = cashSale(h, 1, 1250, 1250)
	expect("is Schedule H and needs a prescription")
	x := w.product(t, "TEST-X-1", "X")
	body = cashSale(x, 1, 1250, 1250)
	expect("Schedule X or NDPS")

	body = cashSale(otc, 11, 14300, 14300)
	expect("not enough sellable stock: TEST-PCM-500 (1 more units needed)")
	body = cashSale(otc, 1, 1299, 1300)
	expect("the tenders come to 1299 but the invoice is 1300")

	code, s, out := w.sell(t, cashSale(otc, 1, 1300, 1300))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, "TS/2627/1", s.InvoiceNumber, "refused sales leave no gap")
	units, _ := w.available(t, otc)
	require.Equal(t, int64(9), units)

	for _, bad := range []string{
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1},{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":2600,"tendered_minor":2600}]}`, otc, otc),
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":1300}]}`, otc),
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"card","amount_minor":1300}]}`, otc),
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":1300,"tendered_minor":1200}]}`, otc),
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":0}],"tenders":[{"kind":"cash","amount_minor":1300,"tendered_minor":1300}]}`, otc),
		`{"lines":[],"tenders":[{"kind":"cash","amount_minor":1300,"tendered_minor":1300}]}`,
	} {
		code, _, out := w.sell(t, bad)
		require.Equal(t, http.StatusBadRequest, code, bad+" → "+out)
	}
}

// Review focus 2: at 00:30 IST it is already 1 November at the outlet, so
// a batch expiring that day is neither offered nor sold.
func TestABatchExpiringTodayIsNotSoldAtLocalMidnight(t *testing.T) {
	w := newSaleWorld(t, true)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 10, 30, 6, 30, 0, 0, time.UTC) })
	w.taxFrom = "2026-10-30"
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2026-11-01", 13000, 10)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 10, 31, 19, 0, 0, 0, time.UTC) })
	units, nearest := w.available(t, product)
	require.Equal(t, int64(0), units)
	require.Nil(t, nearest)
	code, _, out := w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not enough sellable stock")
}

// Two tills selling the last unit: exactly one invoice is posted.
func TestTwoTillsCannotSellTheSameLastUnit(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2028-06-30", 13000, 1)
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", cashSale(product, 1, 1300, 1300)).Code
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			require.Equal(t, http.StatusConflict, c)
		}
	}
	require.Equal(t, 1, created, "%v", codes)
}

func TestTheDatabaseKeepsSaleStockAndSeriesHonest(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	batch := w.shelve(t, product, "2028-06-30", 13000, 10)
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_movements (id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
			VALUES (?,?,?,?,?,'sale',-1,'TEST','sale:none:1','user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, w.sellable, batch).Error
	})
	require.ErrorContains(t, err, "a sale movement takes its line's units off a sellable shelf")

	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var posted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &posted))
	for _, stmt := range []string{
		`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
			schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			SELECT tenant_id,outlet_id,sale_id,2,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,
			supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor FROM pharmacy_sale_lines WHERE sale_id=?`,
		`INSERT INTO pharmacy_sale_tenders (tenant_id,outlet_id,sale_id,tender_number,kind,amount_minor,reference)
			SELECT tenant_id,outlet_id,id,2,'upi',1,'TEST' FROM pharmacy_sales WHERE id=?`,
	} {
		err = catalogueTx(t, w.db, func(tx *gorm.DB) error { return tx.Exec(stmt, posted.ID).Error })
		require.ErrorContains(t, err, "a posted sale cannot be added to")
	}
	// A line's movement must take exactly its units: a header, line and
	// short movement written together are refused.
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		sale := uuid.Must(uuid.NewV7())
		if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
			seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
			SELECT ?,tenant_id,outlet_id,financial_year,99,'TS/2627/99',sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
			currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE id=?`, sale, posted.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
			schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			SELECT tenant_id,outlet_id,?,1,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,
			supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor FROM pharmacy_sale_lines WHERE sale_id=?`, sale, posted.ID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stock_movements (id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
			VALUES (?,?,?,?,?,'sale',-2,'TEST',?,'user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, w.sellable, batch, "sale:"+sale.String()+":1").Error
	})
	require.ErrorContains(t, err, "a sale movement takes its line's units off a sellable shelf")
	// Review I3: a line is checked against its batch and class, not only
	// its own copied values.
	for _, change := range []string{"mrp_minor+1", "schedule_declaration_id"} {
		column := strings.SplitN(change, "+", 2)[0]
		err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
			sale := uuid.Must(uuid.NewV7())
			if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
				seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
				SELECT ?,tenant_id,outlet_id,financial_year,98,'TS/2627/98',sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
				currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE id=?`, sale, posted.ID).Error; err != nil {
				return err
			}
			value := change
			if column == "schedule_declaration_id" {
				// Another product's approved declaration.
				value = `(SELECT id FROM pharmacy_product_schedule_declarations WHERE product_id<>pharmacy_sale_lines.product_id LIMIT 1)`
			}
			return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,
				schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor)
				SELECT tenant_id,outlet_id,?,1,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,`+
				map[bool]string{true: value, false: "schedule_declaration_id"}[column == "schedule_declaration_id"]+`,classification_code,
				supply_nature,rate_ppm,`+map[bool]string{true: value, false: "mrp_minor"}[column == "mrp_minor"]+`,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor
				FROM pharmacy_sale_lines WHERE sale_id=?`, sale, posted.ID).Error
		})
		require.ErrorContains(t, err, "a sale line must be a licensed, sellable batch at its own price", change)
	}
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_invoice_series SET last_number=last_number+2 WHERE tenant_id=?`, testutil.TenantA).Error
	})
	require.ErrorContains(t, err, "an invoice series steps by one")
}

func TestSaleArithmetic(t *testing.T) {
	for _, c := range []struct {
		mrp, pack, qty     int64
		nature             string
		rate               int64
		amount, taxable, h int64
	}{
		{12500, 10, 3, "taxable", 120000, 3750, 3348, 201},
		{999, 7, 3, "taxable", 50000, 428, 408, 10},
		{999, 7, 3, "taxable", 180000, 428, 362, 33},
		{999, 7, 3, "nil_rated", 0, 428, 428, 0},
		// Exactly half a paisa rounds up.
		{2, 1, 1, "taxable", 1000000, 2, 0, 1},
		// At the schema's bounds the product passes int64.
		{1000000000000, 1, 1000000, "taxable", 280000, 1000000000000000000, 781250000000000000, 109375000000000000},
	} {
		amount, taxable, half := pharmacy.PriceLine(c.mrp, c.pack, c.qty, c.nature, c.rate)
		require.Equal(t, []int64{c.amount, c.taxable, c.h}, []int64{amount, taxable, half}, "%+v", c)
		require.Equal(t, amount, taxable+2*half)
	}
	for day, fy := range map[string]string{"2026-04-01": "2627", "2027-03-31": "2627", "2027-04-01": "2728", "2099-12-31": "9900"} {
		got, err := pharmacy.FinancialYear(day)
		require.NoError(t, err)
		require.Equal(t, fy, got, day)
	}
}

// Review I1, I2: a wholesale licence does not license a counter sale, and a
// union territory's invoice is not issued until UTGST is modelled.
func TestOnlyARetailLicenceInAStateSellsAtTheCounter(t *testing.T) {
	w := newSaleWorld(t, false)
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2028-06-30", 13000, 10)
	id := uuid.Must(uuid.NewV7()).String()
	wholesale := strings.Replace(licenceBody("2026-01-01", "2030-12-31"), `"retail"`, `"wholesale"`, 1)
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/licences/"+id), "editor", wholesale).Code)
	require.Equal(t, http.StatusOK, do(w.r, "POST", licencePath("/licences/"+id+"/review"), "pharmacist", `{"decision":"approved","reason":"checked"}`).Code)
	w.profile(t, "TS")
	res := do(w.r, "POST", registrationsPath(pharmaOutlet), "editor", registrationBody("04", "04ABCDE1234F1Z8", "2026-11-01", ""))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	code, _, out := w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "no reviewed retail drug licence valid today")

	w.licence(t)
	code, _, out = w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, out, "union territory (UTGST)")
}

// Review I6: each product refusal, and batches the shared predicate holds
// back from both availability and allocation.
func TestHeldBatchesAndUnsellableProductsAreNeverSold(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	held := w.shelve(t, product, "2027-01-31", 13000, 10)
	w.shelve(t, product, "2028-06-30", 13000, 10)
	res := do(w.r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(held, w.sellable))
	require.Equal(t, 200, res.Code, res.Body.String())
	units, nearest := w.available(t, product)
	require.Equal(t, int64(10), units, "the held batch is not offered")
	require.Equal(t, "2028-06-30", *nearest)
	code, s, out := w.sell(t, cashSale(product, 10, 13000, 13000))
	require.Equal(t, http.StatusCreated, code, out)
	require.NotEqual(t, held, s.Lines[0].BatchID, "nor sold")

	expect := func(product, reason string) {
		t.Helper()
		code, _, out := w.sell(t, cashSale(product, 1, 1300, 1300))
		require.Equal(t, http.StatusConflict, code, out)
		require.Contains(t, out, reason)
		quote := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}]}`, product))
		require.Equal(t, http.StatusConflict, quote.Code, "a quote refuses as the sale does")
		require.Contains(t, quote.Body.String(), reason)
	}
	ndps := w.product(t, "TEST-NDPS-1", "none")
	approveClass(t, w.r, ndps, "none", true)
	expect(ndps, "Schedule X or NDPS")

	untaxed := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", productPath(untaxed), "editor", strings.Replace(productBody(0, "TEST untaxed"), `"TEST-AMOX-500"`, `"TEST-UNTAXED"`, 1)).Code)
	expect(untaxed, "has no tax classification in force")

	undeclared := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", productPath(undeclared), "editor", strings.Replace(productBody(0, "TEST undeclared"), `"TEST-AMOX-500"`, `"TEST-UNDECLARED"`, 1)).Code)
	require.Equal(t, http.StatusCreated, do(w.r, "POST", productPath(undeclared)+"/tax-classifications", "editor", classificationBody("30049099", "taxable", "12", "", "2026-11-01", "")).Code)
	expect(undeclared, "has no approved schedule class")

	stopped := w.product(t, "TEST-STOPPED", "none")
	body := strings.Replace(strings.Replace(productBody(1, "TEST STOPPED"), `"TEST-AMOX-500"`, `"TEST-STOPPED"`, 1), `"status":"active"`, `"status":"discontinued"`, 1)
	res = do(w.r, "PUT", productPath(stopped), "editor", body)
	require.Equal(t, 200, res.Code, res.Body.String())
	expect(stopped, "is discontinued")
}

// Review focus 4 and I6: a replay after the stock moved returns the
// original invoice, concurrent retries of one id post it once, and the
// series restarts on 1 April in the outlet's time.
func TestReplaysAreStableAndTheSeriesFollowsTheFinancialYear(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, product, "2028-06-30", 13000, 30)
	id := uuid.Must(uuid.NewV7()).String()
	body := cashSale(product, 2, 2600, 2600)
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = do(w.r, "PUT", salePath("/"+id), "pharmacist", body).Code })
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			require.Equal(t, http.StatusOK, c)
		}
	}
	require.Equal(t, 1, created, "%v", codes)

	code, _, out := w.sell(t, cashSale(product, 3, 3900, 3900))
	require.Equal(t, http.StatusCreated, code, out)
	approveClass(t, w.r, product, "H", false)
	again := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, again.Code, "a replay is the posted invoice, whatever changed since")
	require.Contains(t, again.Body.String(), `"invoice_number":"TS/2627/1"`)
	require.Contains(t, again.Body.String(), `"quantity_units":2`)

	approveClass(t, w.r, product, "none", false)
	// 31 March 23:30 IST is still 2026-27; 1 April 00:30 IST is 2027-28.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2027, 3, 31, 18, 0, 0, 0, time.UTC) })
	_, s, out := w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, "TS/2627/3", s.InvoiceNumber, out)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2027, 3, 31, 19, 0, 0, 0, time.UTC) })
	_, s, out = w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, "TS/2728/1", s.InvoiceNumber, out)
}
