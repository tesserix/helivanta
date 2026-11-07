package iam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

func boundaryOperatingInput() OperatingDutyInput {
	now := time.Now().In(time.FixedZone("IST", 19800))
	return OperatingDutyInput{
		Scope: OperatingDutyScope{
			EmployerKind: "supplier", EmployerID: uuid.MustParse(boundaryID),
			LocationID: uuid.MustParse(boundaryHospital), ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1,
		},
		Identity: OperatingDutyIdentity{StaffID: uuid.MustParse(boundaryID), StaffVersion: 1, LinkID: uuid.MustParse(boundaryHospital), LinkVersion: 2},
		Subject:  "test-receiver", TemplateKey: "supply.inventory_receiver.v1", Reason: " test receiving duty ",
		ValidFrom: now, ExpiresAt: now.Add(24 * time.Hour),
	}
}

func TestOperatingBoundaryRequiresExactReviewedIdentityScopeAndPrecision(t *testing.T) {
	valid := boundaryOperatingInput()
	require.True(t, validateOperatingInput(&valid, time.Now(), boundaryHospital))
	require.Equal(t, "test receiving duty", valid.Reason)
	require.Same(t, time.UTC, valid.ValidFrom.Location())
	require.Same(t, time.UTC, valid.ExpiresAt.Location())
	require.Zero(t, valid.ValidFrom.Nanosecond()%1000)
	require.Zero(t, valid.ExpiresAt.Nanosecond()%1000)
	for name, change := range map[string]func(*OperatingDutyInput){
		"unknown template":         func(in *OperatingDutyInput) { in.TemplateKey = "unpublished.template" },
		"wrong employer family":    func(in *OperatingDutyInput) { in.Scope.EmployerKind = "pharmacy" },
		"missing employer":         func(in *OperatingDutyInput) { in.Scope.EmployerID = uuid.Nil },
		"missing location":         func(in *OperatingDutyInput) { in.Scope.LocationID = uuid.Nil },
		"unversioned employer":     func(in *OperatingDutyInput) { in.Scope.ExpectedEmployerVersion = 0 },
		"unsafe location revision": func(in *OperatingDutyInput) { in.Scope.ExpectedLocationVersion = 9007199254740992 },
		"unreviewed link":          func(in *OperatingDutyInput) { in.Identity.LinkVersion = 1 },
		"missing Staff":            func(in *OperatingDutyInput) { in.Identity.StaffID = uuid.Nil },
		"unversioned Staff":        func(in *OperatingDutyInput) { in.Identity.StaffVersion = 0 },
		"missing link":             func(in *OperatingDutyInput) { in.Identity.LinkID = uuid.Nil },
		"missing subject":          func(in *OperatingDutyInput) { in.Subject = "" },
		"service subject":          func(in *OperatingDutyInput) { in.Subject = "service:receiver" },
		"missing reason":           func(in *OperatingDutyInput) { in.Reason = " " },
		"control character":        func(in *OperatingDutyInput) { in.Reason = "review\x00hidden" },
		"oversized reason":         func(in *OperatingDutyInput) { in.Reason = strings.Repeat("x", 501) },
		"missing start":            func(in *OperatingDutyInput) { in.ValidFrom = time.Time{} },
		"reversed validity":        func(in *OperatingDutyInput) { in.ExpiresAt = in.ValidFrom.Add(-time.Second) },
		"unbounded validity":       func(in *OperatingDutyInput) { in.ExpiresAt = in.ValidFrom.Add(8784*time.Hour + time.Second) },
		"unbounded future": func(in *OperatingDutyInput) {
			in.ValidFrom = time.Now().AddDate(2, 0, 0)
			in.ExpiresAt = in.ValidFrom.Add(time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := boundaryOperatingInput()
			change(&in)
			require.False(t, validateOperatingInput(&in, time.Now(), boundaryHospital))
		})
	}
	require.False(t, validateOperatingInput(&valid, time.Now(), "not-a-tenant"))
	require.False(t, operatingAdministrationRole(PermOperatingReview, authz.RoleSupplyAuditor))
	require.False(t, operatingAdministrationRole(PermOperatingPrepare, authz.RoleSupplyCompliance))
	require.True(t, operatingAdministrationRole(PermOperatingRead, authz.RoleSupplyAuditor))
	require.True(t, operatingAdministrationRole(PermOperatingReview, authz.RoleSupplyCompliance))
	require.False(t, operatingAdministrationRole(PermOperatingRead, authz.Role("unrecognized-role")))
}

func TestOperatingBoundaryRetainsProposalReceiptWithoutInventingCurrentAccess(t *testing.T) {
	input := boundaryOperatingInput()
	created := time.Date(2026, 11, 1, 10, 0, 0, 0, time.FixedZone("IST", 19800))
	row := operatingDutyRow{
		TenantID: uuid.MustParse(boundaryHospital), ID: uuid.MustParse(boundaryID),
		Input: json.RawMessage(boundaryJSON(t, input)), PreparedBy: "test-preparer",
		Status: "revoked", Version: 8, CreatedAt: created, UpdatedAt: created.Add(24 * time.Hour),
	}
	current, err := operatingDutyView(row, false)
	require.NoError(t, err)
	require.Equal(t, "revoked", current.Status)
	require.EqualValues(t, 8, current.Version)
	receipt, err := operatingDutyView(row, true)
	require.NoError(t, err)
	require.Equal(t, "pending_review", receipt.Status)
	require.EqualValues(t, 1, receipt.Version)
	require.Equal(t, created.UTC(), receipt.UpdatedAt)
	require.Equal(t, current.Input, receipt.Input)
	require.Equal(t, current.PreparedBy, receipt.PreparedBy)
	row.Input = json.RawMessage("{")
	_, err = operatingDutyView(row, true)
	require.Error(t, err)
}

func TestOperatingBoundaryProjectionRejectsForeignOrMalformedEnvelopes(t *testing.T) {
	valid := events.Event{
		TenantID: boundaryHospital, Type: iamcontract.SubjectOperatingDutyChanged, Version: 1,
		Data: json.RawMessage(boundaryJSON(t, iamcontract.OperatingDutyChangedData{DutyID: boundaryID, Version: 1})),
	}
	consumer := operatingProjectionConsumer(platform.Deps{})
	require.NoError(t, consumer.Handle(context.Background(), nil, valid))
	for name, change := range map[string]func(*events.Event){
		"missing tenant":      func(e *events.Event) { e.TenantID = "" },
		"nil tenant":          func(e *events.Event) { e.TenantID = uuid.Nil.String() },
		"wrong topic":         func(e *events.Event) { e.Type = "unrelated.event.v1" },
		"unsupported version": func(e *events.Event) { e.Version = 2 },
		"foreign destination": func(e *events.Event) { e.DestinationTenantID = boundaryID },
		"truncated body":      func(e *events.Event) { e.Data = json.RawMessage("{") },
		"array body":          func(e *events.Event) { e.Data = json.RawMessage("[]") },
		"invalid duty": func(e *events.Event) {
			e.Data = json.RawMessage(boundaryJSON(t, iamcontract.OperatingDutyChangedData{DutyID: "not-a-duty", Version: 1}))
		},
		"unversioned duty": func(e *events.Event) {
			e.Data = json.RawMessage(boundaryJSON(t, iamcontract.OperatingDutyChangedData{DutyID: boundaryID, Version: 0}))
		},
		"unsafe revision": func(e *events.Event) {
			e.Data = json.RawMessage(boundaryJSON(t, iamcontract.OperatingDutyChangedData{DutyID: boundaryID, Version: 9007199254740992}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			event := valid
			change(&event)
			require.Error(t, consumer.Handle(context.Background(), nil, event))
		})
	}
}

func TestOperatingBoundaryMissingAuthoritiesCannotInvokeProtectedAction(t *testing.T) {
	called := false
	operating := NewOperatingAuthorizer(nil, nil, nil, nil)
	require.ErrorIs(t, operating.WithOperatingScope(context.Background(), authz.OperatingScopeRequest{}, func(context.Context, authz.OperatingScopeGrant) error {
		called = true
		return nil
	}), authz.ErrScopedAuthorizationUnavailable)
	partner := NewPartnerScopeAuthorizer(platform.Deps{})
	require.ErrorIs(t, partner.WithPartnerScope(context.Background(), authz.PartnerScopeRequest{}, func(context.Context, authz.PartnerScopeGrant) error {
		called = true
		return nil
	}), authz.ErrScopedAuthorizationUnavailable)
	recipient, ok := partner.(authz.PartnerRecipientSource)
	require.True(t, ok)
	require.ErrorIs(t, recipient.WithCurrentPartnerRecipient(context.Background(), authz.PartnerScopeRequest{}, func(context.Context) error {
		called = true
		return nil
	}), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
}
