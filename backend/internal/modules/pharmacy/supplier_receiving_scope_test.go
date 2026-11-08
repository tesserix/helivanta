package pharmacy

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestReceivingDiscoveryAllowsOnlyPaginationAndCommandsRejectQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query       string
		list, valid bool
	}{
		{"?limit=1", true, true}, {"?cursor=sealed", true, true}, {"?limit=1", false, false},
		{"?outlet=other", true, false}, {"?limit=1&limit=2", true, false}, {"?cursor=", true, false},
	} {
		t.Run(tc.query+map[bool]string{true: " list", false: " command"}[tc.list], func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/receipts"+tc.query, nil)
			p := authn.Principal{Subject: "TEST-reader", TenantID: uuid.NewString()}
			c.Set("authn.principal", p)
			c.Set("authn.entry_refresh", func() bool { return true })
			c.Request.Header.Set("X-Helivanta-Expected-Tenant", p.TenantID)
			c.Request.Header.Set("X-Helivanta-Expected-Subject", p.Subject)
			for _, key := range []string{"hospital", "department", "outlet", "shipment"} {
				c.Params = append(c.Params, gin.Param{Key: key, Value: uuid.Must(uuid.NewV7()).String()})
			}
			_, _, ok := receivingScope(c, tc.list)
			require.Equal(t, tc.valid, ok, response.Body.String())
			if !tc.valid {
				require.Equal(t, 400, response.Code)
			}
		})
	}
}
