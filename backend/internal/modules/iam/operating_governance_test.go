package iam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceOperatingNativeReadCannotAliasLegacyAdmission(t *testing.T) {
	for _, test := range []struct {
		name        string
		permissions []authz.Permission
		status      int
	}{
		{"legacy read", []authz.Permission{PermOperatingRead}, http.StatusForbidden},
		{"legacy review", []authz.Permission{PermOperatingReview}, http.StatusForbidden},
		{"review is not source read", []authz.Permission{authz.Permission(authz.GovernanceOperatingReview)}, http.StatusForbidden},
		{"native source dependency unavailable", []authz.Permission{authz.Permission(authz.GovernanceOperatingManage)}, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := authn.Principal{Subject: "synthetic-native-operator", TenantID: boundaryHospital, AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
			identity := boundaryIdentity{principal: p}
			r := gin.New()
			r.Use(authn.Middleware(identity, identity), authz.Middleware(boundaryPermissions{authz.NewPermissionSet(test.permissions...)}))
			h := &operatingHandlers{}
			r.GET("/duties/:kind/:employer/:id", func(c *gin.Context) {
				native, ok := h.nativeHandler(c)
				if ok {
					native.get(c)
				}
			})
			req := httptest.NewRequest(http.MethodGet, "/duties/pharmacy/"+boundaryHospital+"/"+boundaryID, nil)
			req.Header.Set("Authorization", "Bearer synthetic-session")
			req.Header.Set("X-Helivanta-Expected-Tenant", p.TenantID)
			req.Header.Set("X-Helivanta-Expected-Subject", p.Subject)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			require.Equal(t, test.status, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "owner_membership_id")
		})
	}
}
func TestGovernanceOperatingSourceCannotAdoptAnotherOriginalAuthority(t *testing.T) {
	grant, owner, employer := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	version := int64(2)
	original := operatingGovernanceAuthority{TenantID: boundaryHospital, EmployerKind: "supplier", EmployerID: employer, ActorSubject: "synthetic-native-operator", Kind: authz.GovernanceGrantSource, SourceGrantID: &grant, SourceGrantVersion: &version, MembershipEpoch: 1, Capability: authz.GovernanceOperatingReview}
	fresh := original
	fresh.Capability = authz.GovernanceOperatingManage
	fresh.AuthenticatedAt = time.Now()
	require.True(t, sameOperatingGovernanceSource(original, fresh), "a current session may recover its own source's receipt")
	for name, mutate := range map[string]func(*operatingGovernanceAuthority){
		"replacement grant":          func(v *operatingGovernanceAuthority) { id := uuid.Must(uuid.NewV7()); v.SourceGrantID = &id },
		"revised grant":              func(v *operatingGovernanceAuthority) { version := int64(3); v.SourceGrantVersion = &version },
		"new membership incarnation": func(v *operatingGovernanceAuthority) { v.MembershipEpoch++ },
		"owner cannot adopt grant": func(v *operatingGovernanceAuthority) {
			v.Kind = authz.GovernanceOwnerSource
			v.SourceGrantID = nil
			v.SourceGrantVersion = nil
			v.OwnerMembershipID = &owner
		},
		"other employer":    func(v *operatingGovernanceAuthority) { v.EmployerID = uuid.Must(uuid.NewV7()) },
		"other owner model": func(v *operatingGovernanceAuthority) { v.EmployerKind = "pharmacy" },
		"other actor":       func(v *operatingGovernanceAuthority) { v.ActorSubject = "another-native-operator" },
		"other tenant":      func(v *operatingGovernanceAuthority) { v.TenantID = boundaryID },
	} {
		t.Run(name, func(t *testing.T) {
			changed := original
			mutate(&changed)
			require.False(t, sameOperatingGovernanceSource(original, changed))
		})
	}
	original.Kind = authz.GovernanceOwnerSource
	original.SourceGrantID = nil
	original.SourceGrantVersion = nil
	original.OwnerMembershipID = &owner
	recreated := original
	replacement := uuid.Must(uuid.NewV7())
	recreated.OwnerMembershipID = &replacement
	require.False(t, sameOperatingGovernanceSource(original, recreated))
}
func TestGovernanceOperatingNativeCommandsRequireClosedCapabilities(t *testing.T) {
	for _, permission := range []authz.Permission{PermOperatingRead, PermOperatingPrepare, PermOperatingRevoke} {
		capability, ok := operatingGovernanceCapability(permission)
		require.True(t, ok)
		require.Equal(t, authz.GovernanceOperatingManage, capability)
	}
	capability, ok := operatingGovernanceCapability(PermOperatingReview)
	require.True(t, ok)
	require.Equal(t, authz.GovernanceOperatingReview, capability)
	_, ok = operatingGovernanceCapability(PermMemberManage)
	require.False(t, ok)
	h := &operatingHandlers{native: &operatingNativeRequest{}}
	_, err := h.actorProof(context.Background(), authn.Principal{}, PermMemberManage)
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
}
