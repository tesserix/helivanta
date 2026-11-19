package iam

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
)

func storedMFAIntent(t *testing.T) (customercontract.StaffMFARecoveryRequest, staffMFAIntent) {
	t.Helper()
	in := customercontract.StaffMFARecoveryRequest{ActorSubject: "acting-admin", Target: customercontract.StaffMFARecoveryTarget{
		TenantID: "11111111-1111-4111-8111-111111111111", EmployerKind: "hospital", EmployerID: "11111111-1111-4111-8111-111111111112", StaffID: "11111111-1111-4111-8111-111111111113", StaffVersion: 1, LinkID: "019f39c2-1738-7998-b5de-6d4214f614e1", LinkVersion: 2, Subject: "staff-user",
	}, Intent: customercontract.StaffMFAResetInput{CommandID: "019f39c2-1738-7998-b5de-6d4214f614e0", ExpectedLinkID: "019f39c2-1738-7998-b5de-6d4214f614e1", ExpectedLinkVersion: 2, ExpectedStaffVersion: 1, Reason: "Verified synthetic lost authenticator"}}
	raw, fingerprint, err := staffMFAFingerprint(in)
	require.NoError(t, err)
	return in, staffMFAIntent{TenantID: in.Target.TenantID, ActorSubject: in.ActorSubject, EmployerKind: in.Target.EmployerKind, EmployerID: uuid.MustParse(in.Target.EmployerID), StaffID: uuid.MustParse(in.Target.StaffID), Fingerprint: fingerprint, Request: raw}
}

func TestStaffMFAIntentReconstructsCanonicalJSONBAfterReordering(t *testing.T) {
	original, held := storedMFAIntent(t)
	var object map[string]any
	require.NoError(t, json.Unmarshal(held.Request, &object))
	reordered, err := json.MarshalIndent(object, "", " ")
	require.NoError(t, err)
	require.NotEqual(t, string(held.Request), string(reordered))
	held.Request = reordered
	got, err := held.decode()
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func TestStaffMFAIntentRefusesBrokenFingerprintAndRowScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*staffMFAIntent)
	}{
		{"missing", func(h *staffMFAIntent) { h.Request = nil }},
		{"null", func(h *staffMFAIntent) { h.Request = []byte("null") }},
		{"oversize", func(h *staffMFAIntent) { h.Request = append(h.Request, []byte(strings.Repeat(" ", 8193))...) }},
		{"changed_payload", func(h *staffMFAIntent) {
			h.Request = []byte(strings.Replace(string(h.Request), "acting-admin", "other-admin", 1))
		}},
		{"changed_tenant_column", func(h *staffMFAIntent) { h.TenantID = "11111111-1111-4111-8111-111111111114" }},
		{"changed_actor_column", func(h *staffMFAIntent) { h.ActorSubject = "other-admin" }},
		{"changed_employer_column", func(h *staffMFAIntent) { h.EmployerID = uuid.New() }},
		{"changed_staff_column", func(h *staffMFAIntent) { h.StaffID = uuid.New() }},
		{"changed_kind_column", func(h *staffMFAIntent) { h.EmployerKind = "supplier" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, held := storedMFAIntent(t)
			tc.mutate(&held)
			_, err := held.decode()
			require.ErrorIs(t, err, staffclient.ErrUnavailable)
		})
	}
}
