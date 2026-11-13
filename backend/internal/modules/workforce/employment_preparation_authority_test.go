package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestEmploymentPreparationProofBindsRealDraftAndCommand(t *testing.T) {
	hospital, staff, period := uuid.New(), uuid.New(), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-terms-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 2, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	expected := int64(0)
	input := EmploymentPeriodDraftInput{CommandID: uuid.Must(uuid.NewV7()), ExpectedVersion: &expected, StaffVersion: 2, EmploymentEpoch: 3, EngagementKind: "employee", Duration: "ongoing", StartsAt: time.Now().UTC(), Reason: "Synthetic terms", EvidenceReference: "Synthetic reference"}
	proof, err := employmentPreparationProof(p, hospital, staff, period, preparationSave, &input, source)
	require.NoError(t, err)
	require.Equal(t, period, proof["period_id"])
	require.Equal(t, input.CommandID, proof["command_id"])
	require.Equal(t, expected, proof["expected_version"])
	require.Equal(t, input.StaffVersion, proof["staff_version"])
	require.Len(t, proof["input_hash"], 64)
	for _, key := range []string{"review_id", "actor_staff_id", "actor_link_id", "reason", "evidence_reference", "starts_at"} {
		require.NotContains(t, proof, key)
	}
	_, err = employmentPreparationProof(p, hospital, staff, uuid.New(), preparationSave, &input, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = employmentPreparationProof(p, hospital, staff, period, preparationRead, &input, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = employmentPreparationProof(p, hospital, staff, uuid.Nil, preparationSave, &input, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	read, err := employmentPreparationProof(p, hospital, staff, period, preparationRead, nil, source)
	require.NoError(t, err)
	require.NotContains(t, read, "command_id")
	require.NotContains(t, read, "input_hash")
	_, err = employmentPreparationProof(p, hospital, staff, period, preparationList, nil, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = employmentPreparationProof(p, hospital, staff, uuid.Nil, preparationList, nil, source)
	require.NoError(t, err)
}

func TestEmploymentPreparationReplayRetainsSourceIdentityButRefreshesLease(t *testing.T) {
	original := map[string]any{"purpose": "employment_preparation", "actor_subject": "synthetic", "authority_kind": "grant", "source_grant_id": uuid.NewString(), "source_grant_version": int64(2), "membership_epoch": int64(3), "owner_count": int64(2), "staff_id": uuid.NewString(), "period_id": uuid.Must(uuid.NewV7()).String(), "command_id": uuid.Must(uuid.NewV7()).String(), "input_hash": "synthetic", "authenticated_at": time.Now(), "idle_deadline": time.Now().Add(time.Hour)}
	current := map[string]any{}
	for key, value := range original {
		current[key] = value
	}
	current["authenticated_at"], current["idle_deadline"] = time.Now().Add(time.Minute), time.Now().Add(2*time.Hour)
	require.True(t, sameEmploymentPreparationSource(original, current))
	for _, key := range []string{"actor_subject", "authority_kind", "source_grant_id", "source_grant_version", "membership_epoch", "owner_count", "staff_id", "period_id", "command_id", "input_hash"} {
		saved := current[key]
		current[key] = "changed"
		require.False(t, sameEmploymentPreparationSource(original, current), key)
		current[key] = saved
	}
}

func TestEmploymentPreparationTargetsOmitSelectedStaffAndRejectMixedScope(t *testing.T) {
	hospital := uuid.New()
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	proof, err := employmentPreparationProof(p, hospital, uuid.Nil, uuid.Nil, preparationTargets, nil, source)
	require.NoError(t, err)
	require.Equal(t, preparationTargets, proof["operation"])
	for _, key := range []string{"staff_id", "period_id", "command_id", "review_id", "actor_staff_id", "actor_link_id"} {
		require.NotContains(t, proof, key)
	}
	for _, row := range []struct {
		staff, period uuid.UUID
		operation     employmentPreparationOperation
		input         *EmploymentPeriodDraftInput
	}{
		{uuid.New(), uuid.Nil, preparationTargets, nil},
		{uuid.Nil, uuid.Must(uuid.NewV7()), preparationTargets, nil},
		{uuid.Nil, uuid.Nil, preparationTargets, &EmploymentPeriodDraftInput{}},
		{uuid.Nil, uuid.Nil, preparationSource, nil},
		{uuid.Nil, uuid.Nil, preparationList, nil},
	} {
		_, err := employmentPreparationProof(p, hospital, row.staff, row.period, row.operation, row.input, source)
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	}
}
