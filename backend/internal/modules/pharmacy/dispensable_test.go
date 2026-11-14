package pharmacy_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

// #1708: the balance's dispensable units are what a sale could take from
// that location and batch now, by the sale's own predicate.

func (w releaseWorld) dispensable(t *testing.T, location, batch string) int64 {
	t.Helper()
	res := do(w.r, "GET", stockPath("balance?location_id="+location+"&batch_id="+batch), "editor", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var b struct {
		DispensableUnits *int64 `json:"dispensable_units"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &b))
	require.NotNil(t, b.DispensableUnits, "the balance always reports dispensable units")
	return *b.DispensableUnits
}

func TestBalanceReportsUnitsASaleCouldTake(t *testing.T) {
	w := newReleaseWorld(t)
	code, msg := w.release(w.batch, 30)
	require.Equal(t, http.StatusOK, code, msg)
	require.EqualValues(t, 0, w.dispensable(t, w.quarantine, w.batch), "quarantine is never dispensable")
	require.EqualValues(t, 30, w.dispensable(t, w.sellable, w.batch))
}

func TestBalanceExcludesABatchWithASafetyIncident(t *testing.T) {
	w := newReleaseWorld(t)
	code, msg := w.release(w.batch, 30)
	require.Equal(t, http.StatusOK, code, msg)
	res := do(w.r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(w.batch, w.sellable))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.EqualValues(t, 30, w.balance(t, w.sellable, w.batch), "the units are still on hand")
	require.EqualValues(t, 0, w.dispensable(t, w.sellable, w.batch))
}

func TestBalanceExcludesStockExpiredAtTheOutlet(t *testing.T) {
	w := newReleaseWorld(t)
	// Expires on the outlet's 2 Nov; still sellable on its 1 Nov.
	batch := receive(t, w.r, w.product, w.quarantine, "2026-11-02", `,"mrp_minor":12500`)
	code, msg := w.release(batch, 10)
	require.Equal(t, http.StatusOK, code, msg)
	require.EqualValues(t, 10, w.dispensable(t, w.sellable, batch))
	// 2 Nov 00:30 IST: a batch is sold only before its expiry date.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 19, 0, 0, 0, time.UTC) })
	require.EqualValues(t, 10, w.balance(t, w.sellable, batch))
	require.EqualValues(t, 0, w.dispensable(t, w.sellable, batch))
}
