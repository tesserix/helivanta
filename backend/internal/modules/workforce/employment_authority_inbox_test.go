package workforce

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestEmploymentAuthorityInboxRejectsAmbiguousOrUnboundedInputs(t *testing.T) {
	for _, query := range []string{"mode=activate", "mode=", "mode=review&mode=prepare", "status=all", "status=", "status=pending&status=approved", "staff_id=synthetic", "hr_intake=v1", "patient_id=synthetic"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/?"+query, nil)
			_, _, err := employmentAuthorityInboxInput(c)
			require.ErrorIs(t, err, errIntakeProjection)
		})
	}
	for _, test := range []struct {
		query      string
		permission authz.Permission
		status     string
	}{{"", PermEmploymentReview, "pending"}, {"mode=prepare&status=approved", PermEmploymentPrepare, "approved"}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?"+test.query, nil)
		permission, status, err := employmentAuthorityInboxInput(c)
		require.NoError(t, err)
		require.Equal(t, test.permission, permission)
		require.Equal(t, test.status, status)
	}
}

func TestEmploymentInboxProofIsEmployerBoundAndCannotImpersonateStaff(t *testing.T) {
	hospital := uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-inbox-owner", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().Add(time.Hour)}
	for _, permission := range []authz.Permission{PermEmploymentPrepare, PermEmploymentReview} {
		proof, err := employmentInboxProof(p, hospital, permission, source)
		require.NoError(t, err)
		require.Equal(t, "employment_inbox", proof["purpose"])
		require.Equal(t, hospital, proof["hospital_id"])
		require.Equal(t, source.OwnerMembershipID, proof["owner_membership_id"])
		for _, key := range []string{"staff_id", "review_id", "actor_staff_id", "actor_link_id", "source_grant_id"} {
			require.NotContains(t, proof, key)
		}
	}
	_, err := employmentInboxProof(p, uuid.New(), PermEmploymentReview, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	source.Kind, source.GrantID, source.Version, source.TemplateKey, source.OwnerMembershipID = authz.GovernanceGrantSource, uuid.Must(uuid.NewV7()), 2, "workforce.employment_preparer.v1", uuid.Nil
	_, err = employmentInboxProof(p, hospital, PermEmploymentReview, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	proof, err := employmentInboxProof(p, hospital, PermEmploymentPrepare, source)
	require.NoError(t, err)
	require.NotContains(t, proof, "owner_membership_id")
	require.Equal(t, source.GrantID, proof["source_grant_id"])
}
