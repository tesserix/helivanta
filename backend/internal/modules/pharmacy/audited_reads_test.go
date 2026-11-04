package pharmacy_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1555: every read that names or points to a person is recorded, in the
// read's own transaction.

type readRecord struct {
	ResourceType string `gorm:"column:resource_type"`
	ResourceID   string `gorm:"column:resource_id"`
	Action       string `gorm:"column:action"`
	Fields       string `gorm:"column:fields"`
	Actor        string `gorm:"column:actor_subject"`
}

// readRecords are the audited reads recorded so far, oldest first; the
// registers and the writes' own audit rows are left out.
func readRecords(t *testing.T, w saleWorld) []readRecord {
	t.Helper()
	var out []readRecord
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT resource_type, resource_id::text AS resource_id, action, array_to_string(changed_fields,',') AS fields, actor_subject
 FROM audit_events WHERE action IN ('record_read','list_read','open_lines_read','summary_read') ORDER BY occurred_at, id`).Scan(&out).Error
	}))
	return out
}

func readCode(w saleWorld, who, path string) int {
	return do(w.r, "GET", licencePath(path), who, "").Code
}

func TestEachPersonalReadIsRecordedOnce(t *testing.T) {
	w := newWorkWorld(t)
	// The product helper declares from 1 November; the world ends on the 2nd.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	g := w.product(t, "TEST-G-DRUG", "G")
	w.shelve(t, g, "2028-06-30", 12500, 30)
	rx := w.prescribe(t, g, 10, 1, false)
	var note string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id::text FROM pharmacy_credit_notes WHERE sale_id=? ORDER BY created_at LIMIT 1`, w.walkIn).Scan(&note).Error
	}))
	before := len(readRecords(t, w.saleWorld))

	reads := []struct {
		path                   string
		resourceType, resource string
		action, fields         string
	}{
		{"/sales/" + w.walkIn, "pharmacy_sale", w.walkIn, "record_read", ""},
		{"/sales?limit=2", "pharmacy_sales", pharmaOutlet, "list_read", ""},
		{"/credit-notes/" + note, "pharmacy_credit_note", note, "record_read", ""},
		{"/credit-notes?sale_id=" + w.walkIn, "pharmacy_credit_notes", pharmaOutlet, "list_read", "sale_id"},
		{"/credit-notes", "pharmacy_credit_notes", pharmaOutlet, "list_read", ""},
		{"/prescriptions/" + rx, "pharmacy_prescription", rx, "record_read", ""},
		{"/prescriptions", "pharmacy_prescriptions", pharmaOutlet, "list_read", ""},
		{"/prescriptions/open-lines?product_id=" + g, "pharmacy_prescriptions", pharmaOutlet, "open_lines_read", "product_id"},
		{"/sales/worklist?from=2026-11-01&to=2026-11-02", "pharmacy_sales_worklist", pharmaOutlet, "list_read", "from,to,customer,invoice"},
		{"/sales/" + w.admitted + "/summary", "pharmacy_sale", w.admitted, "summary_read", ""},
	}
	for i, r := range reads {
		require.Equal(t, http.StatusOK, readCode(w.saleWorld, "pharmacist", r.path), r.path)
		got := readRecords(t, w.saleWorld)
		require.Len(t, got, before+i+1, "one record for %s", r.path)
		require.Equal(t, readRecord{ResourceType: r.resourceType, ResourceID: r.resource, Action: r.action, Fields: r.fields, Actor: "user-pharmacist"}, got[len(got)-1], r.path)
	}
}

func TestReadsWithoutAPersonAreNotRecorded(t *testing.T) {
	w := newWorkWorld(t)
	before := len(readRecords(t, w.saleWorld))
	// A page of walk-in and registered-patient sales, and their summaries,
	// carry no pointer to a person.
	for _, path := range []string{
		"/sales/worklist?from=2026-11-01&to=2026-11-02&customer=walk_in",
		"/sales/worklist?from=2026-11-01&to=2026-11-02&customer=patient",
		"/sales/worklist?from=2026-10-01&to=2026-10-31",
		"/sales/" + w.walkIn + "/summary",
		"/sales/" + w.patient + "/summary",
	} {
		require.Equal(t, http.StatusOK, readCode(w.saleWorld, "pharmacist", path), path)
	}
	require.Len(t, readRecords(t, w.saleWorld), before)
	require.Equal(t, http.StatusOK, readCode(w.saleWorld, "pharmacist", "/sales/worklist?from=2026-11-01&to=2026-11-02&customer=admission"))
	require.Len(t, readRecords(t, w.saleWorld), before+1, "a page with an admission sale is")
}

func TestARefusedReadRecordsNothing(t *testing.T) {
	w := newWorkWorld(t)
	before := len(readRecords(t, w.saleWorld))
	for _, c := range []struct {
		who, path string
		code      int
	}{
		{"foreign", "/sales/" + w.walkIn, http.StatusNotFound},
		{"pharmacist", "/sales/nope", http.StatusNotFound},
		{"stocker", "/sales/" + w.walkIn, http.StatusForbidden},
		{"foreign", "/sales", http.StatusNotFound},
		{"pharmacist", "/credit-notes?sale_id=nope", http.StatusBadRequest},
		{"foreign", "/credit-notes", http.StatusNotFound},
		{"owner", "/prescriptions", http.StatusForbidden},
		{"foreign", "/sales/" + w.admitted + "/summary", http.StatusNotFound},
		{"pharmacist", "/sales/worklist?customer=admission", http.StatusBadRequest},
	} {
		require.Equal(t, c.code, readCode(w.saleWorld, c.who, c.path), c.who+" "+c.path)
	}
	require.Len(t, readRecords(t, w.saleWorld), before)
}

// An audit row that cannot be written withholds the data.
func TestAReadWhoseRecordFailsReturnsNothing(t *testing.T) {
	w := newWorkWorld(t)
	pharmacy.SetReadRecordFault(t)
	for _, path := range []string{
		"/sales/" + w.walkIn,
		"/sales",
		"/credit-notes",
		"/sales/" + w.admitted + "/summary",
		"/sales/worklist?from=2026-11-01&to=2026-11-02",
	} {
		res := do(w.r, "GET", licencePath(path), "pharmacist", "")
		require.Equal(t, http.StatusInternalServerError, res.Code, path+res.Body.String())
		for _, leaked := range []string{"TEST Walk In", "9800000001", "B-7", "invoice_number"} {
			require.False(t, strings.Contains(res.Body.String(), leaked), fmt.Sprintf("%s leaked %s", path, leaked))
		}
	}
}
