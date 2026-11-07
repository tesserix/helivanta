package iam_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test exercises its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplierGrantProtocolPreservesBothOwnersAndTerminalConsent(t *testing.T) {
	_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	source, home := uuid.MustParse(testutil.TenantA), uuid.MustParse(testutil.TenantB)
	group, id := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	at := time.Now().UTC().Truncate(time.Microsecond)
	scope := supplieraccess.SupplierGrantScope{SourceTenantID: source, RecipientTenantID: home, HospitalID: uuid.New(), SourceProduct: "supply", SourceOrganizationID: uuid.Must(uuid.NewV7()), SourceLocationID: uuid.Must(uuid.NewV7())}
	input := supplieraccess.SupplierGrantInput{RelationshipID: uuid.Must(uuid.NewV7()), TermsSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RecipientKind: "group", RecipientGroupID: &group, TemplateKey: "partner.catalogue_viewer.v1", ValidFrom: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), Reason: "TEST ONLY hospital team catalogue access"}
	hash, err := supplieraccess.InputHash(scope, input)
	require.NoError(t, err)
	prepared := supplieraccess.SupplierGrant{ID: id, OwnerTenantID: source, Scope: scope, Input: input, InputSha256: hash, Side: "source", Status: "pending_review", Version: 1, AuthorityEpoch: 1, PreparedBy: "TEST-grant-maker", CreatedAt: at, UpdatedAt: at}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), prepared.PreparedBy), source.String(), func(tx *gorm.DB) error {
		if e := iam.SupplierGrantFrameForTest(tx, "source", uuid.Nil); e != nil {
			return e
		}
		return iam.SupplierGrantSaveForTest(tx, prepared, prepared.PreparedBy, nil, true)
	}))
	load := func(tenant uuid.UUID) supplieraccess.SupplierGrant {
		t.Helper()
		var state supplieraccess.SupplierGrant
		require.NoError(t, db.WithTenant(t.Context(), tenant.String(), func(tx *gorm.DB) error {
			if e := iam.SupplierGrantFrameForTest(tx, "observe", scope.HospitalID); e != nil {
				return e
			}
			var e error
			state, e = iam.SupplierGrantLoadForTest(tx, tenant.String(), id)
			return e
		}))
		return state
	}
	wait := func(tenant uuid.UUID, status string) supplieraccess.SupplierGrant {
		t.Helper()
		var state supplieraccess.SupplierGrant
		require.Eventually(t, func() bool {
			e := db.WithTenant(t.Context(), tenant.String(), func(tx *gorm.DB) error {
				if e := iam.SupplierGrantFrameForTest(tx, "observe", scope.HospitalID); e != nil {
					return e
				}
				var e error
				state, e = iam.SupplierGrantLoadForTest(tx, tenant.String(), id)
				return e
			})
			return e == nil && state.Status == status
		}, 20*time.Second, 50*time.Millisecond)
		return state
	}
	change := func(tenant uuid.UUID, op, actor, basis string) {
		t.Helper()
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), actor), tenant.String(), func(tx *gorm.DB) error {
			if e := iam.SupplierGrantFrameForTest(tx, "observe", scope.HospitalID); e != nil {
				return e
			}
			if e := iam.SupplierGrantLockForTest(tx, tenant.String(), id); e != nil {
				return e
			}
			state, e := iam.SupplierGrantLoadForTest(tx, tenant.String(), id)
			if e != nil {
				return e
			}
			next, event, e := supplieraccess.Decide(state, state.Version, op, actor, basis, time.Now())
			if e != nil {
				return e
			}
			if e = iam.SupplierGrantSaveForTest(tx, next, actor, nil, false); e != nil {
				return e
			}
			return iam.SupplierGrantEnqueueForTest(tx, event)
		}))
	}
	change(source, "approve", "TEST-source-reviewer", "")
	incoming := wait(home, "incoming")
	require.Equal(t, int64(1), incoming.Version)
	basis := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	change(home, "approve", "TEST-hospital-reviewer", basis)
	seller := wait(source, "approved")
	buyer := wait(home, "approved")
	require.Equal(t, seller.AuthorityEpoch, buyer.RemoteEpoch)
	require.Equal(t, buyer.AuthorityEpoch, seller.RemoteEpoch)
	require.Equal(t, &basis, seller.HomeBasisSha256)
	change(source, "suspend", "TEST-source-reviewer", "")
	buyer = wait(home, "suspended")
	signature := "TEST-source-reviewer"
	ack := supplieraccess.SupplierGrantExchange{GrantID: id, Scope: scope, Input: input, InputSha256: hash, SenderTenantID: source, SenderEpoch: 2, Phase: "acknowledged", SignatorySubject: signature, HomeBasisSha256: &basis}
	raw, err := json.Marshal(ack)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), signature), source.String(), func(tx *gorm.DB) error {
			return events.Enqueue(tx, iam.SupplierGrantSubjectForTest, events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: iam.SupplierGrantSubjectForTest, Version: 1, OccurredAt: time.Now().UTC(), TenantID: source.String(), DestinationTenantID: home.String(), Data: raw})
		}))
	}
	require.Eventually(t, func() bool {
		var count int64
		e := db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Table("processed_events").Where("consumer=?", "iam-supplier-grant-exchange").Count(&count).Error
		})
		return e == nil && count >= 6
	}, 20*time.Second, 50*time.Millisecond)
	current := load(home)
	require.Equal(t, buyer, current)
	require.NoError(t, db.WithTenant(context.Background(), uuid.NewString(), func(tx *gorm.DB) error {
		if e := iam.SupplierGrantFrameForTest(tx, "source", uuid.Nil); e != nil {
			return e
		}
		var count int64
		if e := tx.Table("iam_supplier_grants").Where("id=?", id).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		return nil
	}))
	require.Error(t, db.WithTenant(tenantdb.WithActor(t.Context(), signature), source.String(), func(tx *gorm.DB) error {
		if e := iam.SupplierGrantFrameForTest(tx, "source", uuid.Nil); e != nil {
			return e
		}
		return tx.Exec(`DELETE FROM iam_supplier_grant_history WHERE grant_id=?`, id).Error
	}))
	require.Error(t, db.WithTenant(tenantdb.WithActor(t.Context(), signature), source.String(), func(tx *gorm.DB) error {
		if e := iam.SupplierGrantFrameForTest(tx, "source", uuid.Nil); e != nil {
			return e
		}
		return tx.Exec(`UPDATE iam_supplier_grants SET document=jsonb_set(document,'{status}','"approved"'),version=version+1 WHERE id=?`, id).Error
	}))
}
