package supply

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

type partnerShipmentIdentity struct{ principal authn.Principal }

func (identity partnerShipmentIdentity) Verify(context.Context, string) (authn.Principal, error) {
	return identity.principal, nil
}
func (partnerShipmentIdentity) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}
func (partnerShipmentIdentity) IsMember(context.Context, string, string) (bool, error) {
	return true, nil
}
func TestPartnerShipmentReadRequiresCurrentAuthorityBeforeOwnerStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := partnerShipmentIdentity{principal: authn.Principal{Subject: "TEST-home-viewer", TenantID: "0199e000-0000-7000-8000-000000000001", AuthTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), IdleDeadline: time.Now().Add(time.Hour)}}
	r.Use(authn.Middleware(identity, identity))
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(r.Group("/v1"), partnerShipmentIdentity{}).WithCursorSealer(sealer), platform.Deps{})
	root := "/v1/supply/hospitals/0199e000-0000-7000-8000-000000000002/partner-grants/pharmacy/0199e000-0000-7000-8000-000000000003/terms/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/shipment-orders"
	for _, path := range []string{root, root + "/0199e000-0000-7000-8000-000000000004/shipments"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest("GET", path+"?limit=1", nil)
			request.Header.Set("Authorization", "Bearer TEST")
			request.Header.Set("X-Helivanta-Expected-Tenant", "0199e000-0000-7000-8000-000000000001")
			request.Header.Set("X-Helivanta-Expected-Subject", "TEST-home-viewer")
			result := httptest.NewRecorder()
			r.ServeHTTP(result, request)
			require.Equal(t, 503, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), "partner_shipment_unavailable")
		})
	}
}
