package globalapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestSupplierOrderHTTPRejectsChangedContextMalformedCommandsAndUnavailableOwners(t *testing.T) {
	// Omitted Workforce/department owners represent a real dependency outage, not a successful authorization stub.
	backend, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "foreign": testutil.TenantB}, Modules: []platform.Module{supply.New()}})
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"supply": server.URL}})
	require.NoError(t, err)
	hospital, department, outlet, order, command := uuid.NewString(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	buyer := "/v1/supply/hospitals/" + hospital + "/departments/" + department + "/outlets/" + outlet + "/purchase-orders/" + order
	seller := "/v1/supply/supplier-orders/" + order
	decision := `{"expected_version":1,"decision":"approve","reason":"TEST independently reviewed order"}`
	type requestCase struct {
		name, method, path, body string
		status                   int
	}
	cases := []requestCase{
		{name: "buyer read owner outage", method: "GET", path: buyer, status: 503},
		{name: "buyer proposal recovery owner outage", method: "GET", path: buyer + "/proposal-receipt", status: 503},
		{name: "buyer command recovery owner outage", method: "GET", path: buyer + "/commands/" + command, status: 503},
		{name: "buyer preparation owner outage", method: "PUT", path: buyer, body: `{}`, status: 503},
		{name: "buyer approval owner outage", method: "PUT", path: buyer + "/commands/" + command, body: decision, status: 503},
		{name: "buyer cancellation owner outage", method: "PUT", path: buyer + "/commands/" + command, body: strings.Replace(decision, "approve", "cancel", 1), status: 503},
		{name: "absent seller order", method: "GET", path: seller, status: 404},
		{name: "absent seller command", method: "GET", path: seller + "/commands/" + command, status: 404},
		{name: "absent seller approval", method: "PUT", path: seller + "/commands/" + command, body: decision, status: 404},
		{name: "source cannot cancel buyer intent", method: "PUT", path: seller + "/commands/" + command, body: strings.Replace(decision, "approve", "cancel", 1), status: 400},
		{name: "invalid buyer decision", method: "PUT", path: buyer + "/commands/" + command, body: strings.Replace(decision, "approve", "activate", 1), status: 400},
		{name: "unsafe expected version", method: "PUT", path: buyer + "/commands/" + command, body: strings.Replace(decision, `"expected_version":1`, `"expected_version":9007199254740992`, 1), status: 400},
		{name: "unexpected query", method: "GET", path: buyer + "?scope=other", status: 400},
		{name: "missing workspace header", method: "GET", path: buyer, status: 409},
		{name: "changed workspace header", method: "GET", path: buyer, status: 409},
		{name: "changed account header", method: "GET", path: buyer, status: 409},
		{name: "duplicate workspace header", method: "GET", path: buyer, status: 409},
	}
	for _, id := range []string{hospital, department, outlet, order} {
		cases = append(cases, requestCase{name: "invalid scope " + id, method: "GET", path: strings.Replace(buyer, id, "not-an-id", 1), status: 404})
	}
	for _, path := range []string{buyer, buyer + "/commands/" + command, seller + "/commands/" + command} {
		for name, body := range map[string]string{"malformed JSON": `{"reason":`, "unknown field": `{"unknown":true}`, "array body": `[]`, "trailing body": `{ } { }`, "oversized body": `{"reason":"` + strings.Repeat("x", 64<<10) + `"}`} {
			cases = append(cases, requestCase{name: name + " " + path, method: "PUT", path: path, body: body, status: 400})
		}
	}
	for _, path := range []string{buyer + "/commands/" + command, seller + "/commands/" + command} {
		cases = append(cases, requestCase{name: "invalid command " + path, method: "GET", path: strings.Replace(path, command, uuid.NewString(), 1), status: 404})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer owner")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			req.Header.Set("X-Helivanta-Expected-Subject", "user-owner")
			switch tc.name {
			case "missing workspace header":
				req.Header.Del("X-Helivanta-Expected-Tenant")
			case "changed workspace header":
				req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
			case "changed account header":
				req.Header.Set("X-Helivanta-Expected-Subject", "user-foreign")
			case "duplicate workspace header":
				req.Header.Add("X-Helivanta-Expected-Tenant", testutil.TenantA)
			}
			response := httptest.NewRecorder()
			gateway.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Contains(t, response.Body.String(), `"error":`)
		})
	}
}
