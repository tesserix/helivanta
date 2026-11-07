package iam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const boundaryID = "0199eaaa-0000-7000-8000-000000000001"
const boundaryHospital = "0199eaaa-0000-7000-8000-000000000002"

type boundaryIdentity struct {
	principal authn.Principal
}

func (v boundaryIdentity) Verify(context.Context, string) (authn.Principal, error) {
	return v.principal, nil
}
func (boundaryIdentity) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

type boundaryPermissions struct{ perms authz.PermissionSet }

func (r boundaryPermissions) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return r.perms, nil
}

func boundaryRequest(t *testing.T, method, pattern, body string, handler gin.HandlerFunc, change func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	now := time.Now()
	identity := boundaryIdentity{authn.Principal{Subject: "test-operator", TenantID: boundaryHospital, AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}}
	r := gin.New()
	r.Use(authn.Middleware(identity, identity), authz.Middleware(boundaryPermissions{authz.NewPermissionSet(
		PermOperatingRead, PermOperatingPrepare, PermOperatingReview, PermOperatingRevoke,
		PermSupplierGrantRead, PermSupplierGrantPrepare, PermSupplierGrantReview,
		PermSupplierGrantAccept, PermSupplierGrantInbox,
	)}))
	r.Handle(method, pattern, handler)
	path := strings.NewReplacer(":id", boundaryID, ":command", boundaryID, ":hospital", boundaryHospital).Replace(pattern)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-session")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", identity.principal.TenantID)
	req.Header.Set("X-Helivanta-Expected-Subject", identity.principal.Subject)
	if change != nil {
		change(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func boundaryJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func TestAccessBoundaryRejectsChangedOrAmbiguousAccountContext(t *testing.T) {
	source := &supplierGrantHandlers{}
	operating := &operatingHandlers{}
	routes := []struct {
		name, method, path, body string
		handler                  gin.HandlerFunc
	}{
		{"source prepare", http.MethodPut, "/iam/supplier-grants/:id", "{}", source.prepare},
		{"source read", http.MethodGet, "/iam/supplier-grants/:id", "", source.getSource},
		{"source decision", http.MethodPut, "/iam/supplier-grants/:id/commands/:command", "{}", source.decideSource},
		{"source receipt", http.MethodGet, "/iam/supplier-grants/:id/commands/:command", "", source.sourceReceipt},
		{"hospital offer", http.MethodGet, "/iam/hospitals/:hospital/supplier-grant-offers/:id", "", source.getHospitalOffer},
		{"hospital decision", http.MethodPut, "/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", "{}", source.decideHospital},
		{"hospital receipt", http.MethodGet, "/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", "", source.hospitalReceipt},
		{"recipient view", http.MethodGet, "/iam/hospitals/:hospital/supplier-grants/:id", "", source.getHospitalGrant},
		{"duty prepare", http.MethodPut, "/iam/operating-duties/:id", "{}", operating.prepare},
		{"duty read", http.MethodGet, "/iam/operating-duties/:id", "", operating.get},
		{"duty proposal", http.MethodGet, "/iam/operating-duties/:id/proposal-receipt", "", operating.proposalReceipt},
		{"duty receipt", http.MethodGet, "/iam/operating-duties/:id/commands/:command", "", operating.getCommand},
		{"duty decision", http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "test review"}), operating.decide},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			for _, header := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
				for _, change := range []string{"missing", "changed", "duplicate"} {
					t.Run(header+"/"+change, func(t *testing.T) {
						w := boundaryRequest(t, route.method, route.path, route.body, route.handler, func(r *http.Request) {
							switch change {
							case "missing":
								r.Header.Del(header)
							case "changed":
								r.Header.Set(header, "different-account")
							case "duplicate":
								r.Header.Add(header, r.Header.Get(header))
							}
						})
						require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
						require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
						require.NotContains(t, w.Body.String(), "test-operator")
					})
				}
			}
		})
	}
}

func TestAccessBoundaryRejectsMalformedCommandsWithoutDependencies(t *testing.T) {
	source := &supplierGrantHandlers{}
	operating := &operatingHandlers{}
	routes := []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/iam/supplier-grants/:id", source.prepare},
		{"/iam/supplier-grants/:id/commands/:command", source.decideSource},
		{"/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", source.decideHospital},
		{"/iam/operating-duties/:id", operating.prepare},
		{"/iam/operating-duties/:id/commands/:command", operating.decide},
	}
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			for name, body := range map[string]string{
				"truncated": "{", "array": "[]", "trailing object": "{}{}",
				"unknown field": "{\"unapproved_scope\":\"other-hospital\"}",
				"oversized":     "{\"reason\":\"" + strings.Repeat("x", 17<<10) + "\"}",
			} {
				t.Run(name, func(t *testing.T) {
					w := boundaryRequest(t, http.MethodPut, route.path, body, route.handler, nil)
					require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				})
			}
		})
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		func(r *http.Request) { r.URL.RawQuery = "source_tenant=other-hospital" },
	} {
		w := boundaryRequest(t, http.MethodPut, "/iam/supplier-grants/:id", "{}", source.prepare, change)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestAccessBoundaryRejectsNoncanonicalCommandIdentifiers(t *testing.T) {
	source := &supplierGrantHandlers{}
	operating := &operatingHandlers{}
	for _, route := range []struct {
		method, path string
		handler      gin.HandlerFunc
	}{
		{http.MethodGet, "/iam/supplier-grants/:id/commands/:command", source.sourceReceipt},
		{http.MethodGet, "/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", source.hospitalReceipt},
		{http.MethodGet, "/iam/operating-duties/:id/commands/:command", operating.getCommand},
	} {
		for _, invalid := range []string{"not-an-id", "11111111-1111-4111-8111-111111111111", strings.ToUpper(boundaryID)} {
			t.Run(route.path+"/"+invalid, func(t *testing.T) {
				w := boundaryRequest(t, route.method, route.path, "", route.handler, func(r *http.Request) {
					r.URL.Path = strings.TrimSuffix(r.URL.Path, boundaryID) + invalid
				})
				require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			})
		}
	}
}

type boundaryRelationshipFailure struct{ err error }

func (g boundaryRelationshipFailure) VerifyRelationship(context.Context, suppliertrade.RelationshipScope) (suppliertrade.RelationshipAuthorization, error) {
	return nil, g.err
}
func (boundaryRelationshipFailure) WithVerifiedRelationship(context.Context, suppliertrade.RelationshipAuthorization, func(context.Context, suppliertrade.Relationship) error) error {
	panic("a failed relationship check must not reach a write")
}

func TestAccessBoundaryRedactsSupplierDependencyFailures(t *testing.T) {
	subject := "test-recipient"
	now := time.Now().UTC()
	input := supplieraccess.SupplierGrantInput{RelationshipID: uuid.MustParse(boundaryID), TermsSha256: strings.Repeat("a", 64), RecipientKind: "subject", RecipientSubject: &subject, TemplateKey: "partner.catalogue_viewer.v1", ValidFrom: now, ExpiresAt: now.Add(time.Hour), Reason: "test procurement"}
	const private = "PRIVATE-COUNTERPARTY-EVIDENCE"
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid", supplieraccess.ErrInvalid, 400},
		{"missing", gorm.ErrRecordNotFound, 404},
		{"unavailable relationship", suppliertrade.ErrIneligible, 404},
		{"version conflict", supplieraccess.ErrConflict, 409},
		{"relationship conflict", suppliertrade.ErrConflict, 409},
		{"unique conflict", &pgconn.PgError{Code: "23505", Detail: private}, 409},
		{"constraint conflict", &pgconn.PgError{Code: "23514", Detail: private}, 409},
		{"revoked session", authz.ErrScopedCredentialRevoked, 401},
		{"current scope denied", authz.ErrScopedCommandDenied, 403},
		{"recipient ineligible", supplieraccess.ErrIneligible, 403},
		{"dependency unavailable", suppliertrade.ErrUnavailable, 503},
		{"unknown storage failure", errors.New(private), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &supplierGrantHandlers{relationships: boundaryRelationshipFailure{fmt.Errorf("%s: %w", private, tc.err)}}
			w := boundaryRequest(t, http.MethodPut, "/iam/supplier-grants/:id", boundaryJSON(t, input), h.prepare, nil)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), private)
			require.NotContains(t, w.Body.String(), subject)
		})
	}
}

func TestAccessBoundaryDutyDecisionCannotInventAuthority(t *testing.T) {
	h := &operatingHandlers{}
	for _, operation := range []string{"approve", "reject", "withdraw", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			input := OperatingDutyDecisionInput{Decision: operation, ExpectedVersion: 1, Reason: "test review"}
			w := boundaryRequest(t, http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, input), h.decide, nil)
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			for _, version := range []int64{-1, 0, 9007199254740991} {
				input.ExpectedVersion = version
				w = boundaryRequest(t, http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, input), h.decide, nil)
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			}
			input.ExpectedVersion = 1
			for _, reason := range []string{"", " ", "\x00", strings.Repeat("x", 501)} {
				input.Reason = reason
				w = boundaryRequest(t, http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, input), h.decide, nil)
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			}
		})
	}
	input := OperatingDutyDecisionInput{Decision: "grant-professional-rights", ExpectedVersion: 1, Reason: "test"}
	w := boundaryRequest(t, http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, input), h.decide, nil)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAccessBoundaryMissingSourcesCannotFabricateReceiptsOrAccess(t *testing.T) {
	source := &supplierGrantHandlers{}
	operating := &operatingHandlers{}
	input := OperatingDutyDecisionInput{Decision: "approve", ExpectedVersion: 1, Reason: "test review"}
	for _, route := range []struct {
		method, path, body string
		handler            gin.HandlerFunc
	}{
		{http.MethodGet, "/iam/supplier-grants/:id", "", source.getSource},
		{http.MethodGet, "/iam/supplier-grants/:id/commands/:command", "", source.sourceReceipt},
		{http.MethodPut, "/iam/supplier-grants/:id/commands/:command", boundaryJSON(t, supplieraccess.SupplierGrantDecisionInput{ExpectedVersion: 1, Operation: "approve", Reason: "test review"}), source.decideSource},
		{http.MethodGet, "/iam/hospitals/:hospital/supplier-grant-offers/:id", "", source.getHospitalOffer},
		{http.MethodGet, "/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", "", source.hospitalReceipt},
		{http.MethodGet, "/iam/hospitals/:hospital/supplier-grants/:id", "", source.getHospitalGrant},
		{http.MethodPut, "/iam/hospitals/:hospital/supplier-grant-offers/:id/commands/:command", boundaryJSON(t, supplieraccess.SupplierGrantHospitalDecisionInput{ExpectedVersion: 1, Operation: "approve", Reason: "test review"}), source.decideHospital},
		{http.MethodGet, "/iam/operating-duties/:id", "", operating.get},
		{http.MethodGet, "/iam/operating-duties/:id/proposal-receipt", "", operating.proposalReceipt},
		{http.MethodGet, "/iam/operating-duties/:id/commands/:command", "", operating.getCommand},
		{http.MethodPut, "/iam/operating-duties/:id", boundaryJSON(t, boundaryOperatingInput()), operating.prepare},
		{http.MethodPut, "/iam/operating-duties/:id/commands/:command", boundaryJSON(t, input), operating.decide},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			w := boundaryRequest(t, route.method, route.path, route.body, route.handler, nil)
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "\"id\"")
			require.NotContains(t, w.Body.String(), "\"status\":\"approved\"")
		})
	}
}

func TestAccessBoundaryOperatingErrorsDoNotExposePrivateEvidence(t *testing.T) {
	const private = "PRIVATE-STAFF-EVIDENCE"
	for _, tc := range []struct {
		err  error
		want int
	}{
		{errOperatingValidity, 400}, {gorm.ErrRecordNotFound, 404},
		{authz.ErrScopedCommandDenied, 404}, {errAssignmentDenied, 403},
		{errProposalConflict, 409}, {&pgconn.PgError{Code: "23505", Detail: private}, 409},
		{&pgconn.PgError{Code: "23514", Detail: private}, 409},
		{authz.ErrScopedCredentialRevoked, 401}, {authz.ErrScopedAuthorizationUnavailable, 503},
		{context.DeadlineExceeded, 503}, {tenantdb.ErrGuardedCommandExpired, 503},
		{errors.New(private), 500}, {&pgconn.PgError{Code: "08006", Detail: private}, 500},
	} {
		t.Run(fmt.Sprintf("%T-%v", tc.err, tc.want), func(t *testing.T) {
			w := boundaryRequest(t, http.MethodGet, "/failure", "", func(c *gin.Context) {
				operatingError(c, fmt.Errorf("%s: %w", private, tc.err))
			}, nil)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), private)
		})
	}
}
