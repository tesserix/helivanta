package iam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestManualIdentityDecodeRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	for _, input := range []string{`null`, `{}`, `{"tenant_id":"invalid"}`, `{"subject":"other-account"}`, `{}` + `{}`, strings.Repeat(" ", manualIdentityImportBodyMaximum+1)} {
		_, err := DecodeManualIdentitySource(strings.NewReader(input))
		require.Error(t, err)
	}
}

func manualIdentityStoreFixture(t *testing.T) (*tenantdb.DB, ManualIdentitySourceImport) {
	t.Helper()

	app, admin, system := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(app, admin, system)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	migrations := append(tenantdb.Migrations(), events.Migrations()...)
	migrations = append(migrations, audit.Migrations()...)
	migrations = append(migrations, New(nil).Migrations()...)
	require.NoError(t, db.Migrate(ctx, migrations))
	in := manualSourceInputFixture()
	now := time.Now().UTC().Truncate(time.Microsecond)
	in.ObservedAt, in.ValidFrom, in.ValidUntil = now.Add(-time.Hour), now.Add(-time.Minute), now.Add(time.Hour)
	require.NoError(t, db.WithTenant(ctx, in.TenantID.String(), func(tx *gorm.DB) error {
		for _, person := range in.Roots {
			for _, account := range person.Accounts {
				if err := tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,'tenant_admin')", in.TenantID, account.Subject).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}))
	return db, in
}

func TestManualIdentityStoreImportIsAtomicOwnerOnlyAndReplayBound(t *testing.T) {
	db, in := manualIdentityStoreFixture(t)
	ctx := t.Context()

	importSource := func(source ManualIdentitySourceImport) (ManualIdentityImportReceipt, error) {
		var result ManualIdentityImportReceipt
		err := db.WithAdmin(ctx, func(tx *gorm.DB) error {
			var err error
			result, err = ImportManualIdentitySource(tx, source)
			return err
		})
		return result, err
	}
	receipt, err := importSource(in)
	require.NoError(t, err)
	require.Equal(t, in.SourceProfileID, receipt.SourceProfileID)
	require.Equal(t, 2, receipt.People)
	require.Equal(t, 2, receipt.Accounts)
	require.Equal(t, "not_evaluated", receipt.ClinicalAuthority)
	again, err := importSource(in)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	require.NoError(t, db.WithTenant(ctx, in.TenantID.String(), func(tx *gorm.DB) error {
		var memberships, grants int64
		if err := tx.Table("iam_members").Count(&memberships).Error; err != nil {
			return err
		}
		if err := tx.Table("iam_governance_grants").Count(&grants).Error; err != nil {
			return err
		}
		require.Equal(t, int64(2), memberships)
		require.Zero(t, grants, "identity import cannot confer a reviewed role")
		return nil
	}))
	changed := in
	changed.Reason = "Different import intent"
	_, err = importSource(changed)
	require.ErrorIs(t, err, errManualIdentityImportConflict)
	require.NoError(t, db.WithTenant(ctx, in.TenantID.String(), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.iam_manual_identity_import','authorized',true)").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("iam_manual_identity_profiles").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "application context cannot impersonate schema owner")
		var support bool
		if err := tx.Raw("SELECT iam_staff_selection_support() AND iam_operating_governance_support()").Scan(&support).Error; err != nil {
			return err
		}
		require.True(t, support, "new identity tables must not invalidate existing IAM purpose guards")

		for _, value := range []string{"v", "vv", " original evidence ", strings.Repeat("😀", 250), strings.Repeat("😀", 251), "\u0085", "\ufeff", "\u200b"} {
			var accepted bool
			if err := tx.Raw("SELECT iam_manual_identity_text(?,500)", value).Scan(&accepted).Error; err != nil {
				return err
			}
			require.Equal(t, manualIdentityText(value, 500), accepted, "SQL and Go must agree on canonical text")
		}
		return nil
	}))
}

func TestManualIdentityDecodeRejectsDuplicateNestedFields(t *testing.T) {
	raw, err := json.Marshal(manualSourceInputFixture())
	require.NoError(t, err)
	duplicate := strings.Replace(string(raw), `"synthetic-examiner"`, `"synthetic-examiner","subject":"other-account"`, 1)
	_, err = DecodeManualIdentitySource(strings.NewReader(duplicate))
	require.Error(t, err)
	_, err = DecodeManualIdentitySource(strings.NewReader(string(raw)))
	require.NoError(t, err)
}

func assertNoManualIdentityFacts(t *testing.T, db *tenantdb.DB, in ManualIdentitySourceImport) {
	t.Helper()
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.tenant_id',?,true),set_config('app.iam_manual_identity_import','authorized',true)", in.TenantID.String()).Error; err != nil {
			return err
		}
		for _, table := range []string{"iam_manual_identity_profiles", "iam_manual_identity_people", "iam_manual_identity_bindings", "iam_manual_identity_imports", "iam_manual_identity_source_heads"} {
			var count int64
			if err := tx.Table(table).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count)
		}
		return nil
	}))
}

func TestManualIdentityStoreRejectsStaleMembershipWithoutPartialFacts(t *testing.T) {
	db, in := manualIdentityStoreFixture(t)
	in.Roots[1].Accounts[0].ExpectedMembershipEpoch++
	err := db.WithAdmin(t.Context(), func(tx *gorm.DB) error { _, err := ImportManualIdentitySource(tx, in); return err })
	require.ErrorIs(t, err, errManualIdentityState)
	assertNoManualIdentityFacts(t, db, in)
}

func TestManualIdentityStoreAuditFailureRollsBackEveryIdentityFact(t *testing.T) {
	db, in := manualIdentityStoreFixture(t)
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`CREATE FUNCTION test_manual_identity_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit outage'; END $$;
  CREATE TRIGGER test_manual_identity_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.resource_type='iam_manual_identity_imports') EXECUTE FUNCTION test_manual_identity_audit_failure()`).Error
	}))
	err := db.WithAdmin(t.Context(), func(tx *gorm.DB) error { _, err := ImportManualIdentitySource(tx, in); return err })
	require.Error(t, err)
	assertNoManualIdentityFacts(t, db, in)
}

func TestManualIdentityStoreApplicationCannotImport(t *testing.T) {
	db, in := manualIdentityStoreFixture(t)
	err := db.WithTenant(t.Context(), in.TenantID.String(), func(tx *gorm.DB) error { _, err := ImportManualIdentitySource(tx, in); return err })
	require.ErrorIs(t, err, errManualIdentityOperatorRequired)
}

func TestManualIdentityStoreExpiryAtCommitRollsBackPreparedImport(t *testing.T) {
	db, in := manualIdentityStoreFixture(t)
	in.ValidUntil = time.Now().UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	err := db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		_, err := ImportManualIdentitySource(tx, in)
		require.NoError(t, err)
		return tx.Exec("SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.01)", in.ValidUntil).Error
	})
	require.Error(t, err, "a prepared import cannot commit expired root assurance")
	assertNoManualIdentityFacts(t, db, in)
}
