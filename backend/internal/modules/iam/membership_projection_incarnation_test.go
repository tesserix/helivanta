package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http"
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
)

func TestReenrolledMembershipRequiresItsOwnGraphProjection(t *testing.T) {
	module := iam.New(nil).SetMembershipIdentityReader(membershipIdentity{})
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{module}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(module))
	authority, err := iam.NewCurrentAuthorization(db, registry, graph)
	require.NoError(t, err)
	original := uuid.New()
	const subject = "synthetic-incarnation-projection-target"
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,?,'staff_member'),(gen_random_uuid(),?,'user-admin','tenant_admin')`, original, testutil.TenantA, subject, testutil.TenantA).Error
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
	removalID := uuid.Must(uuid.NewV7())
	removed := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removalID.String(), "admin", fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":%q,"role_key":"staff_member","expected_membership_epoch":1,"reason":"Remove original incarnation before projection recovery"}`, original, subject))
	require.Equal(t, http.StatusAccepted, removed.Code, removed.Body.String())
	recovered := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), "admin", fmt.Sprintf(`{"operation":"recover","basis_id":%q,"subject":%q,"role_key":"staff_member","expected_membership_epoch":2,"reason":"Recover an ordinary incarnation with fresh identity proof"}`, removalID, subject))
	require.Equal(t, http.StatusAccepted, recovered.Code, recovered.Body.String())
	var receipt iam.MembershipCommand
	require.NoError(t, json.Unmarshal(recovered.Body.Bytes(), &receipt))
	replacement := receipt.ResultMembershipID

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
