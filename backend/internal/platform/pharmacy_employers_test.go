package platform

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type fakePharmacyEmployers struct{ db *tenantdb.DB }

func (fakePharmacyEmployers) CurrentPharmacyEmployer(context.Context, string, uuid.UUID) (authz.PharmacyEmployer, error) {
	return authz.PharmacyEmployer{}, nil
}

type pharmacyEmployerOwner struct{ fakeModule }

func (*pharmacyEmployerOwner) PharmacyEmployers(db *tenantdb.DB) authz.PharmacyEmployerGuard {
	return fakePharmacyEmployers{db: db}
}

func TestPharmacyEmployersBindsOnlyTheSinglePharmacyOwner(t *testing.T) {
	db := &tenantdb.DB{}
	owner := &pharmacyEmployerOwner{fakeModule{name: "pharmacy"}}

	require.Equal(t, fakePharmacyEmployers{db: db}, PharmacyEmployers([]Module{&fakeModule{name: "workforce"}, owner}, db))

	require.Nil(t, PharmacyEmployers([]Module{owner}, nil), "no database must fail closed")
	require.Nil(t, PharmacyEmployers([]Module{&fakeModule{name: "workforce"}}, db), "no owner must fail closed")
	require.Nil(t, PharmacyEmployers([]Module{owner, &pharmacyEmployerOwner{fakeModule{name: "pharmacy"}}}, db), "a duplicated owner must fail closed")
	require.Nil(t, PharmacyEmployers([]Module{&fakeModule{name: "pharmacy"}}, db), "an owner without the guard must fail closed")
	require.Nil(t, PharmacyEmployers([]Module{&pharmacyEmployerOwner{fakeModule{name: "organization"}}}, db), "a guard outside Pharma must not bind")
}
