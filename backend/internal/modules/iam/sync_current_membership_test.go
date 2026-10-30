package iam_test

import (
	"encoding/json"
	"testing"

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

func TestDelayedMembershipGrantDoesNotProjectAbsentMembership(t *testing.T) {
	module := iam.New(nil)
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	data, err := json.Marshal(iamcontract.MemberChangedData{Subject: "synthetic-removed-member", RoleKey: string(authz.RoleStaffMember)})
	require.NoError(t, err)
	for _, consumer := range module.Consumers(platform.Deps{DB: db, Authz: graph}) {
		if consumer.Subject != iamcontract.SubjectMemberGranted {
			continue
		}
		evt := events.Event{Type: "MemberGranted", Version: 1, TenantID: testutil.TenantA, Data: data}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return consumer.Handle(ctx, tx, evt)
		}))
		require.NotNil(t, consumer.AfterCommit)
		require.NoError(t, consumer.AfterCommit(ctx, evt))
		projected, err := graph.IsMember(ctx, "synthetic-removed-member", testutil.TenantA)
		require.NoError(t, err)
		require.False(t, projected, "a late grant is a reconciliation hint, not authority to recreate removed membership")
		return
	}
	t.Fatal("membership grant consumer missing")
}
