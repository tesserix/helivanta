package pharmacy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var lastHarness struct {
	r   *gin.Engine
	db  *tenantdb.DB
	ctx context.Context
}

func useHarness(t *testing.T) {
	t.Helper()
	lastHarness.r, lastHarness.db, _, lastHarness.ctx = setup(t)
}

type dispensePage struct {
	Data []struct {
		PatientName string `json:"patient_name"`
		Status      string `json:"status"`
	} `json:"data"`
	Page struct {
		Total *int `json:"total"`
	} `json:"page"`
}

// seedDispense writes a dispense row directly with explicit times, so the
// ordering assertions do not depend on wall-clock gaps between statements.
func seedDispense(t *testing.T, tenant, name string, created time.Time, dispensed *time.Time) {
	t.Helper()
	db, ctx := lastHarness.db, lastHarness.ctx
	status := "pending"
	if dispensed != nil {
		status = "dispensed"
	}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "test-seed"), tenant, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name, status, dispensed_at, created_at, medication)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, tenant, uuid.NewString(), name, status, dispensed, created, map[bool]string{true: "TEST med"}[dispensed != nil]).Error
	}))
}

func getDispenses(t *testing.T, path, token string) (int, dispensePage) {
	t.Helper()
	w := do(lastHarness.r, "GET", path, token, "")
	var p dispensePage
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	}
	return w.Code, p
}

func names(p dispensePage) []string {
	out := []string{}
	for _, d := range p.Data {
		out = append(out, d.PatientName)
	}
	return out
}

var base = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func at(minutes int) time.Time   { return base.Add(time.Duration(minutes) * time.Minute) }
func ptr(t time.Time) *time.Time { return &t }

// #1295 D8: Waiting is oldest first, so the longest wait is at the top of
// page 1, and its total counts only waiting rows.
func TestWaitingIsOldestFirstWithTotal(t *testing.T) {
	useHarness(t)
	seedDispense(t, testutil.TenantA, "W-second", at(2), nil)
	seedDispense(t, testutil.TenantA, "W-first", at(1), nil)
	seedDispense(t, testutil.TenantA, "W-third", at(3), nil)
	seedDispense(t, testutil.TenantA, "D-one", at(0), ptr(at(10)))
	seedDispense(t, testutil.TenantA, "D-two", at(4), ptr(at(11)))

	code, p := getDispenses(t, "/v1/pharmacy/dispenses/waiting", "tokA")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"W-first", "W-second", "W-third"}, names(p))
	require.NotNil(t, p.Page.Total)
	require.Equal(t, 3, *p.Page.Total)
}

// Dispensed orders by when the medicine was handed over, not when the
// visit arrived: the newest hand-over is first even for an older visit.
func TestDispensedIsNewestByDispenseTime(t *testing.T) {
	useHarness(t)
	seedDispense(t, testutil.TenantA, "early-visit-late-dispense", at(0), ptr(at(30)))
	seedDispense(t, testutil.TenantA, "late-visit-early-dispense", at(5), ptr(at(20)))
	seedDispense(t, testutil.TenantA, "waiting", at(6), nil)

	code, p := getDispenses(t, "/v1/pharmacy/dispenses/dispensed", "tokA")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"early-visit-late-dispense", "late-visit-early-dispense"}, names(p))
	require.Equal(t, 2, *p.Page.Total)
}

// The v1 status list is unchanged: newest first by arrival and no total.
func TestStatusListUnchanged(t *testing.T) {
	useHarness(t)
	seedDispense(t, testutil.TenantA, "older", at(1), nil)
	seedDispense(t, testutil.TenantA, "newer", at(2), nil)

	code, p := getDispenses(t, "/v1/pharmacy/dispenses?status=pending", "tokA")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"newer", "older"}, names(p))
	require.Nil(t, p.Page.Total)
}

func TestDispenseListsOtherTenantInvisible(t *testing.T) {
	useHarness(t)
	seedDispense(t, testutil.TenantA, "tenant-a", at(1), nil)
	seedDispense(t, testutil.TenantA, "tenant-a-done", at(2), ptr(at(3)))

	for _, path := range []string{"/v1/pharmacy/dispenses/waiting", "/v1/pharmacy/dispenses/dispensed"} {
		code, p := getDispenses(t, path, "tokB")
		require.Equal(t, http.StatusOK, code, path)
		require.Empty(t, p.Data, path)
		require.Equal(t, 0, *p.Page.Total, path)
	}
}

// #1295 D9: Waiting searches by patient name, so the patient at the counter
// can be found in a long queue. The match is a case-insensitive substring
// with LIKE wildcards taken literally, and the total counts the matches.
func TestWaitingSearchesPatientName(t *testing.T) {
	useHarness(t)
	seedDispense(t, testutil.TenantA, "Asha Rao", at(1), nil)
	seedDispense(t, testutil.TenantA, "Rohan Iyer", at(2), nil)
	seedDispense(t, testutil.TenantA, "Asha_Mehta 50%", at(3), nil)
	seedDispense(t, testutil.TenantA, "Asha Done", at(4), ptr(at(5)))

	code, p := getDispenses(t, "/v1/pharmacy/dispenses/waiting?q=asha", "tokA")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"Asha Rao", "Asha_Mehta 50%"}, names(p))
	require.Equal(t, 2, *p.Page.Total)

	_, p = getDispenses(t, "/v1/pharmacy/dispenses/waiting?q="+url.QueryEscape("50%"), "tokA")
	require.Equal(t, []string{"Asha_Mehta 50%"}, names(p))
	_, p = getDispenses(t, "/v1/pharmacy/dispenses/waiting?q=a_m", "tokA")
	require.Equal(t, []string{"Asha_Mehta 50%"}, names(p), "_ is literal, not any character")

	code, _ = getDispenses(t, "/v1/pharmacy/dispenses/waiting?q="+strings.Repeat("a", 101), "tokA")
	require.Equal(t, http.StatusBadRequest, code)
	// Dispensed history is unbounded; a search there is refused, not ignored.
	code, _ = getDispenses(t, "/v1/pharmacy/dispenses/dispensed?q=asha", "tokA")
	require.Equal(t, http.StatusBadRequest, code)
}
