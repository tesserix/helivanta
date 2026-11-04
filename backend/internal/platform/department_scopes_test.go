package platform

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type fakeDepartmentScopes struct{ db *tenantdb.DB }

func (fakeDepartmentScopes) CurrentPath(context.Context, string, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (fakeDepartmentScopes) CurrentSubtree(context.Context, string, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (fakeDepartmentScopes) WithinCurrentDepartment(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, func(context.Context) error) error {
	return nil
}

type departmentScopeOwner struct{ fakeModule }

func (*departmentScopeOwner) DepartmentScopes(db *tenantdb.DB) authz.DepartmentScopeGuard {
	return fakeDepartmentScopes{db: db}
}

func TestDepartmentScopesBindsOnlyTheSingleOrganizationOwner(t *testing.T) {
	db := &tenantdb.DB{}
	owner := &departmentScopeOwner{fakeModule{name: "organization"}}

	guard := DepartmentScopes([]Module{&fakeModule{name: "workforce"}, owner}, db)
	require.Equal(t, fakeDepartmentScopes{db: db}, guard)

	require.Nil(t, DepartmentScopes([]Module{owner}, nil), "no database must fail closed")
	require.Nil(t, DepartmentScopes([]Module{&fakeModule{name: "workforce"}}, db), "no owner must fail closed")
	require.Nil(t, DepartmentScopes([]Module{owner, &departmentScopeOwner{fakeModule{name: "organization"}}}, db), "a duplicated owner must fail closed")
	require.Nil(t, DepartmentScopes([]Module{&fakeModule{name: "organization"}}, db), "an owner without the guard must fail closed")
}
