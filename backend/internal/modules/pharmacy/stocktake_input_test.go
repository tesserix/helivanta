package pharmacy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

func TestStocktakeStartHTTPInput(t *testing.T) {
	const quarantine = `"quarantine_location_id":"0199e000-0000-7000-8000-000000000001"`
	const product = `"0199e000-0000-7000-8000-000000000002"`
	for _, tt := range []struct {
		name   string
		body   string
		status int
	}{
		{"all products", `{` + quarantine + `,"selection":"all","product_ids":[],"reason":"Cycle count"}`, 200},
		{"selected products", `{` + quarantine + `,"selection":"selected","product_ids":[` + product + `],"reason":"Cycle count"}`, 200},
		{"ambiguous all selection", `{` + quarantine + `,"selection":"all","product_ids":[` + product + `],"reason":"Cycle count"}`, 400},
		{"empty selection", `{` + quarantine + `,"selection":"selected","product_ids":[],"reason":"Cycle count"}`, 400},
		{"repeated product", `{` + quarantine + `,"selection":"selected","product_ids":[` + product + `,` + product + `],"reason":"Cycle count"}`, 400},
		{"missing products", `{` + quarantine + `,"selection":"all","reason":"Cycle count"}`, 400},
		{"null products", `{` + quarantine + `,"selection":"all","product_ids":null,"reason":"Cycle count"}`, 400},
		{"blank reason", `{` + quarantine + `,"selection":"all","product_ids":[],"reason":"  "}`, 400},
		{"forged authority", `{` + quarantine + `,"selection":"all","product_ids":[],"reason":"Cycle count","actor_subject":"another-person"}`, 400},
		{"duplicate key", `{` + quarantine + `,"selection":"all","selection":"all","product_ids":[],"reason":"Cycle count"}`, 400},
		{"trailing value", `{` + quarantine + `,"selection":"all","product_ids":[],"reason":"Cycle count"} {}`, 400},
		{"nil destination", `{"quarantine_location_id":"00000000-0000-0000-0000-000000000000","selection":"all","product_ids":[],"reason":"Cycle count"}`, 400},
		{"wrong product ID version", `{` + quarantine + `,"selection":"selected","product_ids":["0199e000-0000-4000-8000-000000000002"],"reason":"Cycle count"}`, 400},
		{"oversized encoding", strings.Repeat(" ", 64<<10) + `{` + quarantine + `,"selection":"all","product_ids":[],"reason":"Cycle count"}`, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.PUT("/", func(c *gin.Context) {
				var in StocktakeStartInput
				if !decodeStocktake(c, &in) {
					return
				}
				if !normalizeStocktakeStart(&in) {
					respond.Error(c, 400, "invalid_request", "Choose an exact stocktake scope.")
					return
				}
				respond.OK(c, in)
			})
			req := httptest.NewRequest("PUT", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code, w.Body.String())
		})
	}
}

func TestStocktakeCountHTTPInput(t *testing.T) {
	const line = `"line_id":"0199e000-0000-7000-8000-000000000002"`
	for _, tt := range []struct {
		name   string
		body   string
		status int
	}{
		{"explicit zero", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":0}]}`, 200},
		{"bounded large count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":1000000000000}]}`, 200},
		{"missing count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `}]}`, 400},
		{"null count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":null}]}`, 400},
		{"negative count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":-1}]}`, 400},
		{"fractional count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":1.5}]}`, 400},
		{"out of bounds", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":1000000000001}]}`, 400},
		{"missing expected version", `{"reason":"Shelf count","counts":[{` + line + `,"counted_units":1}]}`, 400},
		{"empty entries", `{"expected_version":0,"reason":"Shelf count","counts":[]}`, 400},
		{"repeated line", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":1},{` + line + `,"counted_units":2}]}`, 400},
		{"blank reason", `{"expected_version":0,"reason":"  ","counts":[{` + line + `,"counted_units":1}]}`, 400},
		{"forged book count", `{"expected_version":0,"reason":"Shelf count","counts":[{` + line + `,"counted_units":1,"book_units":1}]}`, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.PUT("/", func(c *gin.Context) {
				var in StocktakeCountInput
				if !decodeStocktake(c, &in) {
					return
				}
				if !normalizeStocktakeCount(&in) {
					respond.Error(c, 400, "invalid_request", "Provide exact counted quantities and a reason.")
					return
				}
				respond.OK(c, in)
			})
			req := httptest.NewRequest("PUT", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code, w.Body.String())
		})
	}
}

func TestStocktakeReadParameterShape(t *testing.T) {
	for _, tt := range []struct {
		name   string
		path   string
		paged  bool
		status int
	}{
		{"record", "/", false, 200},
		{"page", "/?limit=1", true, 200},
		{"record query", "/?limit=1", false, 400},
		{"undeclared filter", "/?tenant_id=forged", true, 400},
		{"repeated limit", "/?limit=1&limit=2", true, 400},
		{"empty cursor", "/?cursor=", true, 400},
		{"invalid encoding", "/?%=x", true, 400},
		{"invalid record encoding", "/?%=x", false, 400},
		{"ambiguous delimiter", "/?limit=1;limit=2", true, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/", func(c *gin.Context) {
				if stocktakeRead(c, tt.paged) {
					respond.OK(c, "accepted")
				}
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			require.Equal(t, tt.status, w.Code, w.Body.String())
		})
	}
}
