package pharmacy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func seedEmployerOrganization(t *testing.T, db *tenantdb.DB, tenant, code, ownership string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), tenant, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_organizations(id,tenant_id,code,name,ownership_type) VALUES(?,?,?,?,?)`, id, tenant, code, "TEST "+code+" Pharmacy", ownership).Error
	}))
	return id
}

func currentEmployer(t *testing.T, db *tenantdb.DB, pharmacyID uuid.UUID) (authz.PharmacyEmployer, error) {
	t.Helper()
	var employer authz.PharmacyEmployer
	err := db.WithGuardedTenantCommand(t.Context(), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		var err error
		employer, err = pharmacy.New().PharmacyEmployers(db).CurrentPharmacyEmployer(ctx, testutil.TenantA, pharmacyID)
		return err
	})
	return employer, err
}

func TestPharmacyEmployerGuardReturnsStandaloneOrganizationFacts(t *testing.T) {
	_, db, _, _ := catalogueSetup(t)
	for _, ownership := range []string{"independent", "chain"} {
		id := seedEmployerOrganization(t, db, testutil.TenantA, "TEST-"+strings.ToUpper(ownership), ownership)
		employer, err := currentEmployer(t, db, id)
		require.NoError(t, err)
		require.Equal(t, authz.PharmacyEmployer{PharmacyID: id, Name: "TEST TEST-" + strings.ToUpper(ownership) + " Pharmacy", OwnershipType: ownership}, employer)
	}
}

func TestPharmacyEmployerGuardHidesOrganizationsThatCannotEmploy(t *testing.T) {
	_, db, _, _ := catalogueSetup(t)
	for name, id := range map[string]uuid.UUID{
		"hospital owned": seedEmployerOrganization(t, db, testutil.TenantA, "TEST-HOSPITAL-OWNED", "hospital_owned"),
		"foreign tenant": seedEmployerOrganization(t, db, testutil.TenantB, "TEST-FOREIGN", "independent"),
		"missing":        uuid.Must(uuid.NewV7()),
		"not version 7":  uuid.New(),
		"nil":            uuid.Nil,
	} {
		employer, err := currentEmployer(t, db, id)
		require.ErrorIs(t, err, authz.ErrPharmacyEmployerHidden, name)
		require.NotErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, name)
		require.Zero(t, employer, name)
	}
}

func TestPharmacyEmployerGuardFailsClosedOutsideAHealthyGuardedCommand(t *testing.T) {
	_, db, _, _ := catalogueSetup(t)
	id := seedEmployerOrganization(t, db, testutil.TenantA, "TEST-UNGUARDED", "independent")
	guard := pharmacy.New().PharmacyEmployers(db)
	_, err := guard.CurrentPharmacyEmployer(t.Context(), testutil.TenantA, id)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = pharmacy.New().PharmacyEmployers(nil).CurrentPharmacyEmployer(t.Context(), testutil.TenantA, id)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)

	for name, run := range map[string]func(context.Context, *gorm.DB) error{
		"another tenant": func(ctx context.Context, _ *gorm.DB) error {
			_, err := guard.CurrentPharmacyEmployer(ctx, testutil.TenantB, id)
			return err
		},
		"malformed tenant": func(ctx context.Context, _ *gorm.DB) error {
			_, err := guard.CurrentPharmacyEmployer(ctx, " "+testutil.TenantA, id)
			return err
		},
		"store failure": func(ctx context.Context, _ *gorm.DB) error {
			require.NoError(t, db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				require.Error(t, tx.Exec(`SELECT 1/0`).Error)
				return nil
			}))
			_, err := guard.CurrentPharmacyEmployer(ctx, testutil.TenantA, id)
			return err
		},
	} {
		err := db.WithGuardedTenantCommand(t.Context(), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error { return run(ctx, nil) })
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, name)
		require.NotErrorIs(t, err, authz.ErrPharmacyEmployerHidden, name)
	}
}

func TestPharmacyEmployerGuardHoldsTheOrganizationUntilTheCommandEnds(t *testing.T) {
	_, db, _, _ := catalogueSetup(t)
	id := seedEmployerOrganization(t, db, testutil.TenantA, "TEST-LOCKED", "chain")
	lockedOut := func() error {
		return db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`SELECT 1 FROM pharmacy_organizations WHERE tenant_id=? AND id=? FOR UPDATE NOWAIT`, testutil.TenantA, id).Error
		})
	}
	require.NoError(t, db.WithGuardedTenantCommand(t.Context(), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		_, err := pharmacy.New().PharmacyEmployers(db).CurrentPharmacyEmployer(ctx, testutil.TenantA, id)
		require.NoError(t, err)
		var busy *pgconn.PgError
		require.True(t, errors.As(lockedOut(), &busy))
		require.Equal(t, "55P03", busy.Code)
		return nil
	}))
	require.NoError(t, lockedOut())
}
