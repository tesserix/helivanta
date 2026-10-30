package iam_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestReenrolledMembershipRequiresItsOwnGraphProjection(t *testing.T) {
	module := iam.New(nil)
	_, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(module))
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	original := uuid.New()
	const subject = "synthetic-incarnation-projection-target"
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,?,'staff_member')`, original, testutil.TenantA, subject).Error
	}))
	data, err := json.Marshal(iamcontract.MemberChangedData{Subject: subject, RoleKey: string(authz.RoleStaffMember)})
	require.NoError(t, err)
	projected := false
	for _, consumer := range module.Consumers(platform.Deps{DB: db, Bus: bus, Authz: graph}) {
		if consumer.Subject != iamcontract.SubjectMemberGranted {
			continue
		}
		evt := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: "MemberGranted", Version: 1, TenantID: testutil.TenantA, Data: data}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
		if consumer.AfterCommit != nil {
			require.NoError(t, consumer.AfterCommit(ctx, evt))
		}
		projected = true
	}
	require.True(t, projected, "the registered owner consumer must project the original membership")
	member, err := authority.IsMember(ctx, subject, testutil.TenantA)
	require.NoError(t, err)
	require.True(t, member)
	replacement := uuid.New()
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-setup-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND id=?`, testutil.TenantA, original).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,?,'staff_member')`, replacement, testutil.TenantA, subject).Error
	}))
	require.NotEqual(t, original, replacement)
	member, err = authority.IsMember(ctx, subject, testutil.TenantA)
	require.NoError(t, err)
	require.False(t, member, "a new source membership cannot borrow the old incarnation's unrevoked graph projection")
	roles, err := authority.ListRoles(ctx, subject)
	require.NoError(t, err)
	require.Empty(t, roles, "discovery must not borrow old role tuples either")
	for _, consumer := range module.Consumers(platform.Deps{DB: db, Bus: bus, Authz: graph}) {
		if consumer.Subject != iamcontract.SubjectMemberRevoked {
			continue
		}
		evt := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: "MemberRevoked", Version: 1, TenantID: testutil.TenantA, Data: data}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
		require.NotNil(t, consumer.AfterCommit)
		require.NoError(t, consumer.AfterCommit(ctx, evt))
	}
	member, err = authority.IsMember(ctx, subject, testutil.TenantA)
	require.NoError(t, err)
	require.True(t, member, "the old revoke must reconcile and preserve the current replacement")
	roles, err = authority.ListRoles(ctx, subject)
	require.NoError(t, err)
	require.Equal(t, []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleStaffMember}}, roles)
	oldReady, err := graph.IsMembershipReady(ctx, authz.Membership{TenantID: testutil.TenantA, ID: original.String(), Subject: subject, Role: authz.RoleStaffMember, Epoch: 1})
	require.NoError(t, err)
	require.False(t, oldReady)
}
