package pharmacy

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

type stockHoldRegistrationOnly struct{}

func (stockHoldRegistrationOnly) IsMember(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestStockHoldRejectsChangedActorBeforeResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/stock/holds/:hold"
	for _, route := range registered.Routes() {
		if route.Method == "PUT" && route.Path == path {
			r.PUT(path, route.HandlerFunc)
		}
	}
	request := httptest.NewRequest("PUT", "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stock/holds/0199e000-0000-7000-8000-000000000011", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer TEST")
	request.Header.Set("X-Helivanta-Expected-Subject", "TEST-other-actor")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"session_changed"`)
}

func TestStockHoldRejectsDuplicateFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery(), authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	h := catalogueHandlers{}
	r.PUT("/pharmacies/:pharmacy/outlets/:outlet/stock/holds/:hold", h.putStockHold)
	body := `{"batch_id":"0199e000-0000-7000-8000-000000000012","from_location_id":"0199e000-0000-7000-8000-000000000013","quarantine_location_id":"0199e000-0000-7000-8000-000000000014","quantity_units":1,"quantity_units":2,"reason":"TEST hold","physical_disposition":"moved_to_quarantine"}`
	request := httptest.NewRequest("PUT", "/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stock/holds/0199e000-0000-7000-8000-000000000011", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer TEST")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
}

func TestStockHoldRejectsMalformedCommandsBeforeResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := `{"batch_id":"0199e000-0000-7000-8000-000000000012","from_location_id":"0199e000-0000-7000-8000-000000000013","quarantine_location_id":"0199e000-0000-7000-8000-000000000014","quantity_units":1,"reason":"TEST hold","physical_disposition":"moved_to_quarantine"}`
	for _, tc := range []struct{ name, body, query string }{
		{"zero", strings.Replace(base, `"quantity_units":1`, `"quantity_units":0`, 1), ""},
		{"negative", strings.Replace(base, `"quantity_units":1`, `"quantity_units":-1`, 1), ""},
		{"fractional", strings.Replace(base, `"quantity_units":1`, `"quantity_units":1.5`, 1), ""},
		{"over bound", strings.Replace(base, `"quantity_units":1`, `"quantity_units":1000000001`, 1), ""},
		{"null units", strings.Replace(base, `"quantity_units":1`, `"quantity_units":null`, 1), ""},
		{"missing units", strings.Replace(base, `"quantity_units":1,`, "", 1), ""},
		{"no attestation", strings.Replace(base, "moved_to_quarantine", "pending", 1), ""},
		{"same bucket", strings.Replace(base, "8000-000000000014", "8000-000000000013", 1), ""},
		{"invalid batch", strings.Replace(base, "8000-000000000012", "8000-00000000001z", 1), ""},
		{"legacy batch version", strings.Replace(base, "7000-8000-000000000012", "4000-8000-000000000012", 1), ""},
		{"blank reason", strings.Replace(base, "TEST hold", "   ", 1), ""},
		{"control character", strings.Replace(base, "TEST hold", `TEST\nhold`, 1), ""},
		{"unknown field", strings.Replace(base, `"reason":`, `"tenant_id":"forged","reason":`, 1), ""},
		{"two objects", base + base, ""},
		{"not an object", "null", ""},
		{"query", base, "?ignore_reservations=true"},
		{"body bound", strings.Replace(base, "TEST hold", strings.Repeat("a", 65537), 1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
			h := catalogueHandlers{}
			r.PUT("/pharmacies/:pharmacy/outlets/:outlet/stock/holds/:hold", h.putStockHold)
			request := httptest.NewRequest("PUT", "/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stock/holds/0199e000-0000-7000-8000-000000000011"+tc.query, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer TEST")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, 400, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}
