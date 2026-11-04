package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1060: sales by item by lot and by prescriber, and the period's prescribers.

const (
	raoRegistration = "MMC/2011/04567"
	raoCouncil      = "Maharashtra Medical Council"
)

// lotWorld sells H on Dr Rao's prescription (5 units from one lot, then 3
// from the next), H on Dr Iyer's (2 units), and OTC product O (4 units),
// and takes 1 unit of Dr Rao's first sale back.
type lotWorld struct {
	saleWorld
	h, o       string
	lot1, lot2 string
}

func (w saleWorld) prescribeBy(t *testing.T, name, registration, council, product string, units int) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"prescriber_name":%q,"prescriber_registration":%q,"prescriber_council":%q,"prescribed_on":"2026-10-30","patient_name":"TEST Ravi Kumar","prescriber_address":%q,"patient_address":%q,"lines":[{"prescribed_as":"TEST as written","product_id":%q,"quantity_units":%d,"dispensings_allowed":2}]}`,
		name, registration, council, prescriberAddress, patientAddress, product, units)
	res := do(w.r, "PUT", licencePath("/prescriptions/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	return id
}

// lotOf is the lot number receive gives a batch.
func lotOf(batch string) string { return "TEST-" + batch[24:] }

func newLotWorld(t *testing.T) lotWorld {
	t.Helper()
	w := lotWorld{saleWorld: newSaleWorld(t, true)}
	w.h = w.product(t, "TEST-AMOX-500", "H")
	w.o = w.product(t, "TEST-ORS-200", "none")
	// The counter takes the earliest expiry first: lot1 runs out at 5.
	w.lot1 = lotOf(w.shelve(t, w.h, "2028-06-30", 12500, 5))
	w.lot2 = lotOf(w.shelve(t, w.h, "2028-12-31", 12500, 10))
	w.shelve(t, w.o, "2028-06-30", 12500, 10)
	rao := w.prescribeBy(t, "TEST Dr Asha Rao", raoRegistration, raoCouncil, w.h, 20)
	iyer := w.prescribeBy(t, "TEST Dr K Iyer", "KMC/2015/11", "Karnataka Medical Council", w.h, 2)
	first := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+first), "pharmacist", rxSale(w.h, 5, 6250, rao, ""))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	for _, body := range []string{rxSale(w.h, 3, 3750, rao, ""), rxSale(w.h, 2, 2500, iyer, ""), cashSale(w.o, 4, 5000, 5000)} {
		code, _, out := w.sell(t, body)
		require.Equal(t, http.StatusCreated, code, out)
	}
	code, _, out := w.returnUnits(t, returnBody(first, 1, w.quarantine, 1250))
	require.Equal(t, http.StatusCreated, code, out)
	return w
}

func (w lotWorld) items(t *testing.T, who, filter string) (int, []itemRow, string) {
	t.Helper()
	code, body, out := reportPage[itemRow](t, w.saleWorld, who, "/reports/items?from=2026-11-01&to=2026-11-01"+filter)
	return code, body.Data, out
}

func itemSalesReads(t *testing.T, w saleWorld, resource string) (count int64, fields []string) {
	t.Helper()
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type=? AND actor_subject='user-pharmacist'`, resource).Scan(&count).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT DISTINCT action||':'||array_to_string(changed_fields,',') FROM audit_events WHERE resource_type=?`, resource).Scan(&fields).Error
	}))
	return count, fields
}

func TestSalesByItemFiltersByLot(t *testing.T) {
	w := newLotWorld(t)
	code, rows, out := w.items(t, "pharmacist", "&lot_number="+url.QueryEscape(strings.ToUpper(w.lot1)))
	require.Equal(t, http.StatusOK, code, out)
	require.Equal(t, []soldReturned{{w.h, 5, 1}}, itemTotals(rows), "another case matches; the return reverses a line of this lot")
	_, rows, out = w.items(t, "pharmacist", "&lot_number="+url.QueryEscape(" "+w.lot2+" "))
	require.Equal(t, []soldReturned{{w.h, 5, 0}}, itemTotals(rows), out)
	_, rows, _ = w.items(t, "pharmacist", "&lot_number=TEST-NO-SUCH-LOT")
	require.Empty(t, rows)
	code, _, _ = w.items(t, "pharmacist", "&lot_number="+strings.Repeat("x", 101))
	require.Equal(t, http.StatusBadRequest, code)
	reads, _ := itemSalesReads(t, w.saleWorld, "pharmacy_item_sales")
	require.Zero(t, reads, "a lot is not personal: no audit")
}

func TestSalesByItemFiltersByPrescriber(t *testing.T) {
	w := newLotWorld(t)
	rao := "&prescriber_registration=" + url.QueryEscape(raoRegistration) + "&prescriber_council=" + url.QueryEscape(raoCouncil)
	code, rows, out := w.items(t, "pharmacist", rao)
	require.Equal(t, http.StatusOK, code, out)
	require.Equal(t, []soldReturned{{w.h, 8, 1}}, itemTotals(rows), "Dr Rao's two sales and the return of one; never Dr Iyer's or the OTC sale")
	require.EqualValues(t, 6250+3750-1250, rows[0].NetMinor)
	_, rows, _ = w.items(t, "pharmacist", rao+"&lot_number="+url.QueryEscape(w.lot2))
	require.Equal(t, []soldReturned{{w.h, 3, 0}}, itemTotals(rows), "the filters combine")
	_, rows, _ = w.items(t, "pharmacist", "&prescriber_registration="+url.QueryEscape(raoRegistration)+"&prescriber_council=Karnataka%20Medical%20Council")
	require.Empty(t, rows, "a registration is only an identity with its council")

	reads, fields := itemSalesReads(t, w.saleWorld, "pharmacy_item_sales")
	require.EqualValues(t, 3, reads, "every prescriber-filtered page is recorded")
	require.Equal(t, []string{"report_read:from,to,prescriber_registration,prescriber_council"}, fields, "field names only")

	for _, bad := range []string{"&prescriber_registration=" + url.QueryEscape(raoRegistration), "&prescriber_council=" + url.QueryEscape(raoCouncil), "&prescriber_registration=a;b&prescriber_council=c"} {
		code, _, _ := w.items(t, "pharmacist", bad)
		require.Equal(t, http.StatusBadRequest, code, bad)
	}
	// The owner reads sales but not prescriptions.
	code, _, out = w.items(t, "owner", rao)
	require.Equal(t, http.StatusForbidden, code, out)
	code, rows, _ = w.items(t, "owner", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, rows, 2, "without the filter the owner still reads sales by item")

	pharmacy.SetReadRecordFault(t)
	res := do(w.r, "GET", licencePath("/reports/items?from=2026-11-01&to=2026-11-01"+rao), "pharmacist", "")
	require.Equal(t, http.StatusInternalServerError, res.Code)
	require.NotContains(t, res.Body.String(), w.h, "no record, no data")
}

func TestThePrescribersOfAPeriod(t *testing.T) {
	w := newLotWorld(t)
	// A later prescription spells Dr Rao's name another way; it is still one prescriber.
	again := w.prescribeBy(t, "TEST Dr A. Rao", raoRegistration, raoCouncil, w.h, 1)
	code, _, out := w.sell(t, rxSale(w.h, 1, 1250, again, ""))
	require.Equal(t, http.StatusCreated, code, out)

	res := do(w.r, "GET", licencePath("/reports/prescribers?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var list struct {
		Data []struct {
			Name         string `json:"prescriber_name"`
			Registration string `json:"prescriber_registration"`
			Council      string `json:"prescriber_council"`
		} `json:"data"`
		More bool `json:"more"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.False(t, list.More)
	require.Len(t, list.Data, 2, res.Body.String())
	require.Equal(t, "TEST Dr A. Rao", list.Data[0].Name, "the name last recorded, ordered by name")
	require.Equal(t, raoRegistration, list.Data[0].Registration)
	require.Equal(t, "TEST Dr K Iyer", list.Data[1].Name)

	empty := do(w.r, "GET", licencePath("/reports/prescribers?from=2026-11-02&to=2026-11-02"), "pharmacist", "")
	require.Contains(t, empty.Body.String(), `"data":[]`, "a sale on another day does not list its prescriber")
	reads, fields := itemSalesReads(t, w.saleWorld, "pharmacy_prescribers")
	require.EqualValues(t, 2, reads)
	require.Equal(t, []string{"list_read:from,to"}, fields)

	for _, who := range []string{"owner", "rxreader"} {
		require.Equal(t, http.StatusForbidden, do(w.r, "GET", licencePath("/reports/prescribers?from=2026-11-01&to=2026-11-01"), who, "").Code, who+" holds only one of the two reads")
	}
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", licencePath("/reports/prescribers?from=2026-11-01&to=2026-11-01"), "foreign", "").Code)
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/reports/prescribers?from=2026-11-02&to=2026-11-01"), "pharmacist", "").Code)
}
