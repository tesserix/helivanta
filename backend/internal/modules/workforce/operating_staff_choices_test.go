package workforce

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestGovernanceOperatingStaffChoicesRequireCurrentExactSource(t *testing.T) {
	root := establishment{kind: pharmacyEstablishmentKind, id: uuid.Must(uuid.NewV7())}
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-operator", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: root.kind, ID: &root.id}, GrantID: uuid.Must(uuid.NewV7()), Version: 2, TemplateKey: "iam.operating_access_reviewer.v1", MembershipEpoch: 1, OwnerCount: 2, ExpiresAt: time.Now().Add(time.Hour)}
	proof, err := operatingStaffChoiceProof(p, root, source)
	require.NoError(t, err)
	require.Equal(t, "staff_choices", proof["purpose"])
	require.Equal(t, p.Subject, proof["actor_subject"])
	require.Equal(t, root.id, proof["employer_id"])
	owner := source
	owner.Kind = authz.GovernanceOwnerSource
	owner.OwnerMembershipID = uuid.New()
	owner.GrantID = uuid.Nil
	owner.Version = 0
	owner.TemplateKey = ""
	ownerProof, err := operatingStaffChoiceProof(p, root, owner)
	require.NoError(t, err)
	require.Equal(t, owner.OwnerMembershipID, ownerProof["owner_membership_id"])
	require.NotContains(t, ownerProof, "source_grant_id")
	require.NotContains(t, ownerProof, "source_grant_version")
	require.NotContains(t, ownerProof, "template_key")

	for name, alter := range map[string]func(*authn.Principal, *establishment, *authz.GovernanceSourceEvidence){
		"hospital": func(_ *authn.Principal, r *establishment, _ *authz.GovernanceSourceEvidence) {
			r.kind = hospitalEstablishmentKind
		},
		"another tenant": func(p *authn.Principal, _ *establishment, _ *authz.GovernanceSourceEvidence) {
			p.TenantID = uuid.NewString()
		},
		"another employer": func(_ *authn.Principal, r *establishment, _ *authz.GovernanceSourceEvidence) {
			r.id = uuid.Must(uuid.NewV7())
		},
		"expired source": func(_ *authn.Principal, _ *establishment, s *authz.GovernanceSourceEvidence) {
			s.ExpiresAt = time.Now().Add(-time.Minute)
		},
		"expired session": func(p *authn.Principal, _ *establishment, _ *authz.GovernanceSourceEvidence) {
			p.IdleDeadline = time.Now().Add(-time.Minute)
		},
		"unrelated template": func(_ *authn.Principal, _ *establishment, s *authz.GovernanceSourceEvidence) {
			s.TemplateKey = "workforce.account_link_reviewer.v1"
		},
		"unknown source": func(_ *authn.Principal, _ *establishment, s *authz.GovernanceSourceEvidence) { s.Kind = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			current, selected, evidence := p, root, source
			alter(&current, &selected, &evidence)
			value, err := operatingStaffChoiceProof(current, selected, evidence)
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.Nil(t, value)
		})
	}
}

func TestGovernanceOperatingStaffChoicesNeverListsWithoutIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/workforce/operating-governance-employers/pharmacy/019a55c0-1111-7111-8111-111111111112/staff-choices", nil)
	h := &handlers{}
	rows, err := h.listOperatingStaffChoices(c, pagination.Params{Limit: 20})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestGovernanceOperatingStaffChoiceQueryRejectsAmbiguousFilters(t *testing.T) {
	for _, query := range []string{"?subject=another-user", "?status=inactive", "?status=active&status=active", "?limit=1&limit=20", "?cursor=", "?status=%zz", "?limit=20;status=active"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
			require.False(t, operatingStaffChoiceQuery(c))
		})
	}
	for _, query := range []string{"", "?status=active", "?limit=20&status=active", "?cursor=sealed-anchor&limit=1"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/choices"+query, nil)
			require.True(t, operatingStaffChoiceQuery(c))
		})
	}
}
