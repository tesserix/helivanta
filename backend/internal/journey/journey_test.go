// Package journey holds the phase 2 cross-module integration test:
// medicore visit_created reaches lab via JetStream; pharmacy no longer
// consumes it (#1680).
package journey

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/lab"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

var (
	do = testutil.Do
)

func TestVisitFansOutToLabOnly(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {
				medicore.PermVisitCreate, medicore.PermVisitRead,
				pharmacy.PermDispenseRead, lab.PermOrderRead,
			},
			"tokB": {pharmacy.PermDispenseRead, lab.PermOrderRead},
		},
		Modules: []platform.Module{medicore.New(), pharmacy.New(), lab.New()},
	})

	// Create a visit as tenant A.
	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Lab opens pending work for tenant A. Pharmacy no longer does: its
	// free-text dispense queue is retired (#1680).
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), created.ID)
	}, 30*time.Second, 200*time.Millisecond, "lab should open a pending order")
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), created.ID)

	// Tenant B sees none of it.
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), created.ID)
}
