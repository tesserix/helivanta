package workforce

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"maps"
	"testing"
	"time"
)

func TestEmploymentBindingProofBindsCanonicalRevisionAndCommand(t *testing.T) {
	hospital := uuid.New()
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-native-evidence", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	selected := employmentBindingSelection{Staff: uuid.New(), Period: uuid.Must(uuid.NewV7()), Binding: uuid.Must(uuid.NewV7()), PeriodVersion: 2, StaffVersion: 3, EmploymentEpoch: 4, Operation: bindingPreparationAttach}
	command := employmentBindingCommand{ID: uuid.Must(uuid.NewV7()), Document: uuid.New(), Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	proof, err := employmentBindingProof(p, hospital, selected, &command, source)
	require.NoError(t, err)
	require.Equal(t, "employment_binding_preparation", proof["purpose"])
	require.Equal(t, selected.Period, proof["period_id"])
	require.Equal(t, int64(2), proof["period_version"])
	require.Equal(t, command.ID, proof["command_id"])
	require.Equal(t, command.Document, proof["file_id"])
	for _, key := range []string{"actor_staff_id", "actor_link_id", "review_id", "filename", "reason", "sha256"} {
		require.NotContains(t, proof, key)
	}
	for _, mutate := range []func(*employmentBindingSelection){
		func(v *employmentBindingSelection) { v.Staff = uuid.Nil },
		func(v *employmentBindingSelection) { v.Period = uuid.New() },
		func(v *employmentBindingSelection) { v.Binding = uuid.New() },
		func(v *employmentBindingSelection) { v.PeriodVersion = 0 },
		func(v *employmentBindingSelection) { v.StaffVersion = 0 },
		func(v *employmentBindingSelection) { v.EmploymentEpoch = 9007199254740991 },
		func(v *employmentBindingSelection) { v.Operation = "unknown" },
	} {
		invalid := selected
		mutate(&invalid)
		_, err := employmentBindingProof(p, hospital, invalid, &command, source)
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	}
	selected.Operation = bindingPreparationList
	selected.Binding = uuid.Nil
	_, err = employmentBindingProof(p, hospital, selected, &command, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	listed, err := employmentBindingProof(p, hospital, selected, nil, source)
	require.NoError(t, err)
	require.NotContains(t, listed, "command_id")
	require.NotContains(t, listed, "file_id")
	require.NotContains(t, listed, "binding_id")
	selected.Binding = uuid.Must(uuid.NewV7())
	selected.Operation = bindingPreparationWithdraw
	_, err = employmentBindingProof(p, hospital, selected, &command, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	command.Document = uuid.Nil
	command.ExpectedVersion = 1
	withdrawn, err := employmentBindingProof(p, hospital, selected, &command, source)
	require.NoError(t, err)
	require.Equal(t, int64(1), withdrawn["expected_version"])
}

func TestEmploymentBindingSourceDoesNotAdoptChangedReceipt(t *testing.T) {
	hospital := uuid.New()
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-receipt-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	selected := employmentBindingSelection{Staff: uuid.New(), Period: uuid.Must(uuid.NewV7()), Binding: uuid.Must(uuid.NewV7()), PeriodVersion: 1, StaffVersion: 2, EmploymentEpoch: 3, Operation: bindingPreparationAttach}
	command := employmentBindingCommand{ID: uuid.Must(uuid.NewV7()), Document: uuid.New(), Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	original, err := employmentBindingProof(p, hospital, selected, &command, source)
	require.NoError(t, err)
	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var retained map[string]any
	require.NoError(t, json.Unmarshal(encoded, &retained))
	require.True(t, sameEmploymentBindingSource(retained, original), "immutable JSON provenance and the live typed source agree")
	for _, key := range []string{"purpose", "operation", "tenant_id", "hospital_id", "staff_id", "period_id", "period_version", "binding_id", "command_id", "file_id", "expected_version", "staff_version", "employment_epoch", "input_hash", "actor_subject", "owner_membership_id", "membership_epoch", "owner_count"} {
		changed := maps.Clone(original)
		changed[key] = "changed"
		require.False(t, sameEmploymentBindingSource(retained, changed), key)
	}
	refreshed := maps.Clone(original)
	refreshed["phase"] = int64(3)
	refreshed["source_expires_at"] = time.Now().Add(time.Minute)
	require.True(t, sameEmploymentBindingSource(retained, refreshed), "a fresh lease for the same source may complete a storage phase")
}
func TestEmploymentBindingCommandCannotMixOperations(t *testing.T) {
	g := &employmentPreparationBindings{selected: employmentBindingSelection{Operation: bindingPreparationAttach}}
	input := EmploymentEvidenceAttachInput{CommandID: uuid.Must(uuid.NewV7()), DocumentID: uuid.New(), Reason: "Synthetic binding"}
	digest, err := employmentEvidenceIntent("attach", input)
	require.NoError(t, err)
	require.ErrorIs(t, g.prepare("withdraw", input, digest), authz.ErrScopedAuthorizationUnavailable)
	require.Nil(t, g.command)
	require.NoError(t, g.prepare("attach", input, digest))
	require.Equal(t, input.CommandID, g.command.ID)
	require.Equal(t, input.DocumentID, g.command.Document)
	require.Equal(t, digest, g.command.Digest)
	require.ErrorIs(t, g.prepare("attach", EmploymentEvidenceWithdrawInput{}, digest), authz.ErrScopedAuthorizationUnavailable)
}
