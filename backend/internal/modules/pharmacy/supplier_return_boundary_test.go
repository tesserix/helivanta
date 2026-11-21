package pharmacy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestSupplierReturnRejectsChangedActorBeforeResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns/:return"
	for _, route := range registered.Routes() {
		if route.Method == "PUT" && route.Path == path {
			r.PUT(path, route.HandlerFunc)
		}
	}
	request := httptest.NewRequest("PUT", "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/accounts/supplier-returns/0199e000-0000-7000-8000-000000000011", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer TEST")
	request.Header.Set("X-Helivanta-Expected-Subject", "TEST-other-actor")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"session_changed"`)
}

func TestSupplierReturnInputDoesNotMergeDuplicatesOrInventCredit(t *testing.T) {
	quantity, credit := int64(4), int64(1500)
	receipt := uuid.MustParse("0199e000-0000-7000-8000-000000000012")
	bucket := uuid.MustParse("0199e000-0000-7000-8000-000000000013")
	line := SupplierReturnDraftLineInput{ReceiptID: receipt, QuarantineLocationID: bucket, QuantityUnits: &quantity, ExpectedCreditMinor: &credit}
	input := SupplierReturnDraftInput{PayableID: uuid.MustParse("0199e000-0000-7000-8000-000000000014"), Lines: []SupplierReturnDraftLineInput{line}, Reason: "TEST commercial return"}
	total, ok := normalizeSupplierReturn(&input)
	require.True(t, ok)
	require.Equal(t, credit, total)
	duplicate := input
	duplicate.Lines = []SupplierReturnDraftLineInput{line, line}
	_, ok = normalizeSupplierReturn(&duplicate)
	require.False(t, ok)
	excessive := int64(600000000001)
	first, second := line, line
	first.ExpectedCreditMinor = &excessive
	second.ExpectedCreditMinor = &excessive
	second.ReceiptID = uuid.MustParse("0199e000-0000-7000-8000-000000000015")
	over := input
	over.Lines = []SupplierReturnDraftLineInput{first, second}
	_, ok = normalizeSupplierReturn(&over)
	require.False(t, ok)
	second.ExpectedCreditMinor = &credit
	a, b := input, input
	a.Lines = []SupplierReturnDraftLineInput{line, second}
	b.Lines = []SupplierReturnDraftLineInput{second, line}
	_, ok = normalizeSupplierReturn(&a)
	require.True(t, ok)
	_, ok = normalizeSupplierReturn(&b)
	require.True(t, ok)
	original, err := supplierReturnHash("TEST-owner", a)
	require.NoError(t, err)
	reordered, err := supplierReturnHash("TEST-owner", b)
	require.NoError(t, err)
	require.Equal(t, original, reordered)
	changed, err := supplierReturnHash("TEST-other-owner", b)
	require.NoError(t, err)
	require.NotEqual(t, original, changed)
}

func TestSupplierReturnDefaultRolesRestrictCommercialAuthority(t *testing.T) {
	seen := 0
	for _, grant := range New().Permissions() {
		if grant.Permission == PermSupplierReturnRead || grant.Permission == PermSupplierReturnPrepare {
			seen++
			require.ElementsMatch(t, []authz.Role{authz.RolePharmacyOwner, authz.RolePharmacyPurchasing}, grant.Roles)
		}
	}
	require.Equal(t, 2, seen)
}

func TestSupplierReturnHistoryRejectsUnboundedQueryBeforeResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	sealer, err := pagination.NewSealer([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	New().Routes(platform.NewRouter(registered.Group("/v1"), stockHoldRegistrationOnly{}).WithCursorSealer(sealer), platform.Deps{})
	r := gin.New()
	r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
	path := "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns"
	for _, route := range registered.Routes() {
		if route.Method == "GET" && route.Path == path {
			r.GET(path, route.HandlerFunc)
		}
	}
	for _, query := range []string{"?unknown=all", "?limit=1&limit=2", "?cursor=", "?limit=%zz"} {
		req := httptest.NewRequest("GET", "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/accounts/supplier-returns"+query, nil)
		req.Header.Set("Authorization", "Bearer TEST")
		req.Header.Set("X-Helivanta-Expected-Subject", "TEST-actor")
		req.Header.Set("X-Helivanta-Expected-Tenant", "0199e000-0000-7000-8000-000000000001")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
}
