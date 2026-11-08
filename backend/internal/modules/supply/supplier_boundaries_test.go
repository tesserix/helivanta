package supply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
)

type boundaryVerifier struct{ principal authn.Principal }

func (v boundaryVerifier) Verify(context.Context, string) (authn.Principal, error) {
	return v.principal, nil
}

type boundaryRevocation struct{}

func (boundaryRevocation) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

type supplierBoundary struct {
	name    string
	handler gin.HandlerFunc
	buyer   bool
	body    string
}

func supplierBoundaries() []supplierBoundary {
	h := &purchaseOrderHandlers{bus: &events.Bus{}}
	return []supplierBoundary{
		{"buyer order", h.getBuyer, true, ""},
		{"buyer proposal", h.getBuyerProposal, true, ""},
		{"buyer command", h.getBuyerCommand, true, ""},
		{"buyer preparation", h.prepareBuyer, true, `{}`},
		{"buyer decision", h.decideBuyer, true, `{"decision":"approve","expected_version":1,"reason":"reviewed"}`},
		{"buyer orders", boundaryList(h.listBuyer), true, ""},
		{"buyer shipments", boundaryList(h.listBuyerShipments), true, ""},
		{"buyer response history", boundaryList(h.listBuyerReceivingResponses), true, ""},
		{"seller order", h.getSeller, false, ""},
		{"seller command", h.getSellerCommand, false, ""},
		{"seller decision", h.decideSeller, false, `{"decision":"approve","expected_version":1,"reason":"reviewed"}`},
		{"seller orders", boundaryList(h.listSeller), false, ""},
		{"seller receiving", h.getSupplierReceiving, false, ""},
		{"seller receiving reply", h.respondReceiving, false, `{"expected_summary_version":1,"response":"confirmed","reason":"reviewed"}`},
		{"seller original reply", h.getOwnReceivingResponse, false, ""},
		{"seller response history", boundaryList(h.listSourceReceivingResponses), false, ""},
		{"seller review access", h.sourceReviewAccess, false, ""},
		{"seller reply access", h.sourceResponseAccess, false, ""},
	}
}

func boundaryParams(buyer bool) gin.Params {
	values := map[string]string{
		"product": "supply", "organization": "019a55c0-1111-7111-8111-111111111111", "location": "019a55c0-1111-7111-8111-111111111112",
		"order": "019a55c0-1111-7111-8111-111111111113", "proposal": "019a55c0-1111-7111-8111-111111111114", "command": "019a55c0-1111-7111-8111-111111111115", "shipment": "019a55c0-1111-7111-8111-111111111116", "response": "019a55c0-1111-7111-8111-111111111117", "productID": "019a55c0-1111-7111-8111-111111111119",
	}
	if buyer {
		for _, key := range []string{"hospital", "department", "pharmacy", "outlet"} {
			values[key] = "019a55c0-1111-7111-8111-111111111120"
		}
	}
	params := make(gin.Params, 0, len(values))
	for key, value := range values {
		params = append(params, gin.Param{Key: key, Value: value})
	}
	return params
}

func boundaryRequest(t *testing.T, entry supplierBoundary, params gin.Params, body, query string, identity string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	principal := authn.Principal{Subject: "source-staff", TenantID: "019a55c0-1111-7111-8111-111111111121", AuthTime: time.Now().UTC(), IdleDeadline: time.Now().Add(time.Hour)}
	router := gin.New()
	router.Use(authn.Middleware(boundaryVerifier{principal}, boundaryRevocation{}))
	router.Any("/boundary", func(c *gin.Context) { c.Params = params; entry.handler(c) })
	method := http.MethodGet
	if body != "" {
		method = http.MethodPut
	}
	request := httptest.NewRequest(method, "/boundary"+query, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer boundary-token")
	request.Header.Set("Content-Type", "application/json")
	if identity != "missing" {
		request.Header.Set("X-Helivanta-Expected-Tenant", principal.TenantID)
		request.Header.Set("X-Helivanta-Expected-Subject", principal.Subject)
	}
	if identity == "tenant" {
		request.Header.Set("X-Helivanta-Expected-Tenant", uuid.NewString())
	}
	if identity == "subject" {
		request.Header.Set("X-Helivanta-Expected-Subject", "another-account")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	return response
}

func TestSupplierHTTPBoundariesRejectStaleIdentityBeforeOwnerAccess(t *testing.T) {
	for _, entry := range supplierBoundaries() {
		t.Run(entry.name, func(t *testing.T) {
			for _, identity := range []string{"missing", "tenant", "subject"} {
				t.Run(identity, func(t *testing.T) {
					result := boundaryRequest(t, entry, boundaryParams(entry.buyer), entry.body, "", identity)
					require.Equal(t, http.StatusConflict, result.Code, result.Body.String())
					require.Contains(t, result.Body.String(), "context_changed")
				})
			}
		})
	}
}

func TestSupplierHTTPBoundariesFailClosedWithoutOwnerDependencies(t *testing.T) {
	for _, entry := range supplierBoundaries() {
		t.Run(entry.name, func(t *testing.T) {
			result := boundaryRequest(t, entry, boundaryParams(entry.buyer), entry.body, "", "")
			require.Equal(t, http.StatusServiceUnavailable, result.Code, result.Body.String())
			require.NotContains(t, result.Body.String(), "source-staff")
		})
	}
}

func TestSupplierHTTPBoundariesRejectMalformedResourceScope(t *testing.T) {
	for _, entry := range supplierBoundaries() {
		t.Run(entry.name, func(t *testing.T) {
			keys := []string{"organization", "location"}
			if entry.buyer {
				keys = []string{"hospital", "department", "outlet"}
			}
			for _, key := range keys {
				t.Run(key, func(t *testing.T) {
					params := boundaryParams(entry.buyer)
					for i := range params {
						if params[i].Key == key {
							params[i].Value = "private-source-not-an-id"
						}
					}
					result := boundaryRequest(t, entry, params, entry.body, "", "")
					require.Equal(t, http.StatusNotFound, result.Code, result.Body.String())
					require.NotContains(t, result.Body.String(), "private-source-not-an-id")
				})
			}
		})
	}
}

func TestSupplierReceivingReplyRejectsAmbiguousCommandsBeforePersistence(t *testing.T) {
	entry := supplierBoundary{name: "reply", handler: (&purchaseOrderHandlers{bus: &events.Bus{}}).respondReceiving}
	valid := `{"expected_summary_version":1,"response":"confirmed","reason":"reviewed"}`
	tests := map[string]string{
		"malformed": "{", "unknown field": strings.TrimSuffix(valid, "}") + `,"hospital_count":1}`,
		"unversioned summary":  strings.ReplaceAll(valid, `"expected_summary_version":1`, `"expected_summary_version":0`),
		"unsupported decision": strings.ReplaceAll(valid, `"confirmed"`, `"clear_stock"`),
		"blank reason":         strings.ReplaceAll(valid, `"reviewed"`, `"  "`),
		"oversized reason":     strings.ReplaceAll(valid, "reviewed", strings.Repeat("x", 501)),
		"oversized body":       strings.Repeat("x", 64<<10+1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			result := boundaryRequest(t, entry, boundaryParams(false), body, "", "")
			require.Equal(t, http.StatusBadRequest, result.Code, result.Body.String())
		})
	}
	missingBus := supplierBoundary{handler: (&purchaseOrderHandlers{}).respondReceiving}
	require.Equal(t, http.StatusServiceUnavailable, boundaryRequest(t, missingBus, boundaryParams(false), valid, "", "").Code)
}

func TestSupplierWorklistsRejectAmbiguousOrUnknownQueries(t *testing.T) {
	h := &purchaseOrderHandlers{}
	entries := []supplierBoundary{
		{"buyer orders", boundaryList(h.listBuyer), true, ""},
		{"seller orders", boundaryList(h.listSeller), false, ""},
		{"buyer shipments", boundaryList(h.listBuyerShipments), true, ""},
		{"buyer replies", boundaryList(h.listBuyerReceivingResponses), true, ""},
		{"seller replies", boundaryList(h.listSourceReceivingResponses), false, ""},
	}
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			for _, query := range []string{"?limit=1&limit=2", "?cursor=", "?extra=private"} {
				t.Run(url.QueryEscape(query), func(t *testing.T) {
					result := boundaryRequest(t, entry, boundaryParams(entry.buyer), "", query, "")
					require.Equal(t, http.StatusBadRequest, result.Code, result.Body.String())
				})
			}
		})
	}
}

func boundaryList[T any](read func(*gin.Context, pagination.Params) ([]T, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, err := read(c, pagination.Params{Limit: 50})
		if err != nil {
			purchaseOrderError(c, err)
		}
	}
}

func TestSupplierCommandsRejectMissingImmutableLineage(t *testing.T) {
	required := map[string][]string{
		"buyer order": {"order"}, "buyer proposal": {"order"}, "buyer command": {"order", "command"},
		"buyer preparation": {"order"}, "buyer decision": {"order", "command"},
		"seller order": {"order"}, "seller command": {"order", "command"}, "seller decision": {"order", "command"},
		"seller receiving": {"order", "shipment"}, "seller receiving reply": {"order", "shipment", "response"},
		"seller original reply": {"order", "shipment", "response"},
	}
	for _, entry := range supplierBoundaries() {
		for _, key := range required[entry.name] {
			for _, bad := range []string{uuid.Nil.String(), "not-a-resource"} {
				t.Run(entry.name+"/"+key+"/"+bad, func(t *testing.T) {
					params := boundaryParams(entry.buyer)
					for i := range params {
						if params[i].Key == key {
							params[i].Value = bad
						}
					}
					result := boundaryRequest(t, entry, params, entry.body, "", "")
					require.Equal(t, http.StatusNotFound, result.Code, result.Body.String())
				})
			}
		}
	}
}

func TestHospitalSupplierPathsRequireBothDepartmentAndHospitalIdentity(t *testing.T) {
	for _, entry := range supplierBoundaries() {
		if entry.buyer {
			continue
		}
		for _, badKey := range []string{"", "hospital", "department"} {
			t.Run(entry.name+"/"+badKey, func(t *testing.T) {
				params := boundaryParams(false)
				for _, key := range []string{"hospital", "department"} {
					value := "019a55c0-1111-7111-8111-111111111120"
					if key == badKey {
						value = "not-a-resource"
					}
					params = append(params, gin.Param{Key: key, Value: value})
				}
				result := boundaryRequest(t, entry, params, entry.body, "", "")
				expected := http.StatusServiceUnavailable
				if badKey != "" {
					expected = http.StatusNotFound
				}
				require.Equal(t, expected, result.Code, result.Body.String())
			})
		}
	}
}

func TestSupplierDecisionsRejectUnversionedOrAmbiguousIntent(t *testing.T) {
	for _, entry := range supplierBoundaries() {
		if entry.name != "buyer decision" && entry.name != "seller decision" {
			continue
		}
		for name, body := range map[string]string{
			"invalid JSON": "{", "duplicate command": entry.body + entry.body,
			"unknown authority field": strings.TrimSuffix(entry.body, "}") + `,"staff_id":"untrusted"}`,
			"unversioned":             strings.ReplaceAll(entry.body, `"expected_version":1`, `"expected_version":0`),
			"unsafe precision":        strings.ReplaceAll(entry.body, `"expected_version":1`, `"expected_version":9007199254740992`),
			"unrecognized action":     strings.ReplaceAll(entry.body, `"approve"`, `"dispatch"`),
			"missing reason":          strings.ReplaceAll(entry.body, `"reviewed"`, `"  "`),
		} {
			t.Run(entry.name+"/"+name, func(t *testing.T) {
				result := boundaryRequest(t, entry, boundaryParams(entry.buyer), body, "", "")
				require.Equal(t, http.StatusBadRequest, result.Code, result.Body.String())
			})
		}
	}
}
