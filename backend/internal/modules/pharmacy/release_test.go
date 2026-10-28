package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
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
)

// #1386 A3: stock becomes sellable only by a checked release from
// quarantine.

var releasePerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage",
	"pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.quarantine", "pharmacy.stock.release", "pharmacy.stock.correct",
	"pharmacy.schedule.declare", "pharmacy.schedule.review"}

type releaseWorld struct {
	r                             *gin.Engine
	product, quarantine, sellable string
	batch                         string
}

// releaseWorld is an outlet on 1 Nov 2026 (IST) with a classified, active
// product whose class H is approved, a quarantine and a sellable location,
// and one received batch of 100 with an MRP.
func newReleaseWorld(t *testing.T) releaseWorld {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "clerk": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"editor":     releasePerms,
			"pharmacist": releasePerms,
			"clerk":      {"pharmacy.catalogue.read", "pharmacy.stock.read", "pharmacy.stock.receive"},
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, s := range []string{"user-editor", "user-pharmacist", "user-clerk"} {
		grantCatalogue(t, r, s, "editor")
	}
	w := releaseWorld{r: r, product: uuid.Must(uuid.NewV7()).String(), quarantine: uuid.Must(uuid.NewV7()).String(), sellable: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, do(r, "PUT", productPath(w.product), "editor", productBody(0, "TEST Amoxil")).Code)
	classify(t, r, w.product)
	approveClass(t, r, w.product, "H", false)
	require.Equal(t, 200, do(r, "PUT", stockPath("locations/"+w.quarantine), "editor", `{"code":"TEST-Q","name":"TEST Quarantine"}`).Code)
	res := do(r, "PUT", stockPath("locations/"+w.sellable), "editor", `{"code":"TEST-SHELF","name":"TEST Shelf","stock_status":"sellable"}`)
	require.Equal(t, 200, res.Code, res.Body.String())
	w.batch = receive(t, r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
	return w
}

func classify(t *testing.T, r *gin.Engine, product string) {
	t.Helper()
	res := do(r, "POST", productPath(product)+"/tax-classifications", "editor", classificationBody("30041010", "taxable", "5", "", "2026-11-01", ""))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
}

func approveClass(t *testing.T, r *gin.Engine, product, class string, ndps bool) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(r, "PUT", productPath(product)+"/schedule/"+id, "editor", fmt.Sprintf(`{"regulatory_class":%q,"ndps":%t,"source":"TEST pack label"}`, class, ndps))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.Equal(t, http.StatusOK, do(r, "POST", productPath(product)+"/schedule/"+id+"/review", "pharmacist", `{"decision":"approved","reason":"checked"}`).Code)
}

// receive creates a batch with the given expiry and extra fields (MRP) and
// receives 100 units into the quarantine location.
func receive(t *testing.T, r *gin.Engine, product, location, expires, extra string) string {
	t.Helper()
	batch := uuid.Must(uuid.NewV7()).String()
	res := do(r, "PUT", stockPath("batches/"+batch), "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-%s","expires_on":%q%s}`, product, batch[24:], expires, extra))
	require.Equal(t, 200, res.Code, res.Body.String())
	res = do(r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor",
		fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":100,"reason":"TEST receipt","source_reference":"TEST-GRN-%s"}`, location, batch, batch[24:]))
	require.Equal(t, 200, res.Code, res.Body.String())
	return batch
}

func (w releaseWorld) release(batch string, quantity int) (int, string) {
	res := do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":%d,"reason":"TEST checked and shelved"}`, batch, w.quarantine, w.sellable, quantity))
	return res.Code, res.Body.String()
}

func (w releaseWorld) balance(t *testing.T, location, batch string) int64 {
	t.Helper()
	res := do(w.r, "GET", stockPath("balance?location_id="+location+"&batch_id="+batch), "editor", "")
	require.Equal(t, 200, res.Code, res.Body.String())
	var b struct {
		OnHandUnits int64 `json:"on_hand_units"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &b))
	return b.OnHandUnits
}

// Review focus 5: a release moves units, never creates or overdraws them.
func TestAReleaseMovesStockFromQuarantineToSellable(t *testing.T) {
	w := newReleaseWorld(t)
	id := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":30,"reason":"TEST checked and shelved"}`, w.batch, w.quarantine, w.sellable)
	res := do(w.r, "PUT", stockPath("releases/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"regulatory_class":"H"`)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", stockPath("releases/"+id), "pharmacist", body).Code, "replay")
	require.EqualValues(t, 70, w.balance(t, w.quarantine, w.batch))
	require.EqualValues(t, 30, w.balance(t, w.sellable, w.batch), "replay moved nothing twice")
	res = do(w.r, "GET", stockPath("balance?location_id="+w.sellable+"&batch_id="+w.batch), "editor", "")
	require.Contains(t, res.Body.String(), `"stock_status":"sellable"`)
	require.Contains(t, res.Body.String(), `"dispensable_units":0`)

	code, msg := w.release(w.batch, 71)
	require.Equal(t, http.StatusConflict, code, msg)
	require.EqualValues(t, 70, w.balance(t, w.quarantine, w.batch), "a refused overdraft moves nothing")
	require.EqualValues(t, 30, w.balance(t, w.sellable, w.batch))

	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "clerk", body).Code)

	// Nothing reaches a sellable location except by release.
	res = do(w.r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor",
		fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":5,"reason":"TEST","source_reference":"TEST-GRN-SHELF"}`, w.sellable, w.batch))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "quarantine only")
}

// Review focus 4: each unsafe batch is refused, with the reason.
func TestUnsafeBatchesAreNotReleased(t *testing.T) {
	w := newReleaseWorld(t)
	expired := receive(t, w.r, w.product, w.quarantine, "2026-11-01", `,"mrp_minor":12500`)
	code, msg := w.release(expired, 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "which is not after today")

	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", stockPath("batches/"+uuid.Must(uuid.NewV7()).String()), "editor",
		fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-NOMRP","expires_on":"2030-01-01"}`, w.product)).Code, "a new batch needs its MRP")

	incident := receive(t, w.r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
	res := do(w.r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(incident, w.quarantine))
	require.Equal(t, 200, res.Code, res.Body.String())
	code, msg = w.release(incident, 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "safety incident")

	untaxed := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", productPath(untaxed), "editor", strings.Replace(productBody(0, "TEST Untaxed"), "TEST-AMOX-500", "TEST-UNTAXED", 1)).Code)
	approveClass(t, w.r, untaxed, "none", false)
	code, msg = w.release(receive(t, w.r, untaxed, w.quarantine, "2030-10-31", `,"mrp_minor":5000`), 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "no tax classification")

	unclassed := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", productPath(unclassed), "editor", strings.Replace(productBody(0, "TEST Unclassed"), "TEST-AMOX-500", "TEST-UNCLASSED", 1)).Code)
	classify(t, w.r, unclassed)
	code, msg = w.release(receive(t, w.r, unclassed, w.quarantine, "2030-10-31", `,"mrp_minor":5000`), 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "no approved schedule class")

	narcotic := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(w.r, "PUT", productPath(narcotic), "editor", strings.Replace(productBody(0, "TEST Narcotic"), "TEST-AMOX-500", "TEST-NARCOTIC", 1)).Code)
	classify(t, w.r, narcotic)
	approveClass(t, w.r, narcotic, "H", true)
	code, msg = w.release(receive(t, w.r, narcotic, w.quarantine, "2030-10-31", `,"mrp_minor":5000`), 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "NDPS")

	// Stock from the wrong direction, and a discontinued product.
	res = do(w.r, "PUT", stockPath("releases/"+uuid.Must(uuid.NewV7()).String()), "pharmacist",
		fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":1,"reason":"TEST"}`, w.batch, w.sellable, w.quarantine))
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "from a quarantine location to a sellable one")
	require.Equal(t, 200, do(w.r, "PUT", productPath(w.product), "editor", strings.Replace(productBody(1, "TEST Amoxil"), `"status":"active"`, `"status":"discontinued"`, 1)).Code)
	code, msg = w.release(w.batch, 1)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, msg, "the product is discontinued")
}
