package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGovernanceEmploymentBasisSourceRetainsOrdinaryMembershipAndExactTarget(t *testing.T) {
	native := &governanceNativeIdentity{}
	modules := []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New()}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: modules, Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview}}})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES
   (?,'user-admin','tenant_admin'),(?,'synthetic-ordinary','staff_member'),
   (?,'synthetic-delegate','staff_member'),(?,'synthetic-delegate','governance_delegate'),
   (?,'synthetic-delegate-only','governance_delegate'),(?,'synthetic-clinical','staff_member'),(?,'synthetic-clinical','doctor'),(?,'user-reviewer','staff_member')`,
			testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	body, _ := json.Marshal(map[string]string{"name": "Synthetic employment basis hospital"})
	created := governanceRequest(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", string(body))
	require.Equal(t, 200, created.Code, created.Body.String())
	guard, ok := platform.GovernanceSources(modules, db).(authz.GovernanceEmploymentActivationGuard)
	require.True(t, ok)
	req := authz.GovernanceSourceRequest{Principal: authn.Principal{TenantID: testutil.TenantA, Subject: "user-admin", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}, Capability: authz.GovernanceEmploymentActivate, Scope: authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "hospital", ID: &hospital}}
	basis := func(subject string) authz.EmploymentBasis {
		return authz.EmploymentBasis{TenantID: testutil.TenantA, Subject: subject, HospitalID: hospital, StaffID: uuid.Must(uuid.NewV7()), GenerationID: uuid.Must(uuid.NewV7()), EmploymentEpoch: 2, Until: time.Now().Add(time.Hour)}
	}
	var escaped authz.EmploymentBasisWriter
	var escapedContext context.Context
	readAuthority := func(b authz.EmploymentBasis) map[string]any {
		t.Helper()
		var raw string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT authority::text FROM iam_employment_basis_authorities WHERE tenant_id=? AND generation_id=?`, testutil.TenantA, b.GenerationID).Scan(&raw).Error
		}))
		var proof map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &proof), "basis source proof must actually exist")
		require.Equal(t, hospital.String(), proof["hospital_id"])
		require.Equal(t, b.StaffID.String(), proof["staff_id"])
		require.Equal(t, string(authz.GovernanceEmploymentActivate), proof["capability"])
		return proof
	}

	initial := map[string]authz.EmploymentBasis{}
	for _, subject := range []string{"synthetic-ordinary", "synthetic-delegate"} {
		b := basis(subject)
		err := guard.WithGovernanceEmploymentActivation(ctx, req, b.StaffID, func(commandCtx context.Context, source authz.GovernanceSourceEvidence, writer authz.EmploymentBasisWriter) error {
			require.Equal(t, authz.GovernanceOwnerSource, source.Kind)
			require.Equal(t, hospital, *source.Scope.ID)
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var free bool
				if err := tx.Raw(`SELECT pg_try_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.employment:"+testutil.TenantA+":"+hospital.String()).Scan(&free).Error; err != nil {
					return err
				}
				require.False(t, free, "governance activation holds the same exclusive basis lock as scoped activation")
				return nil
			}))
			escaped, escapedContext = writer, commandCtx
			wrong := b
			wrong.StaffID = uuid.Must(uuid.NewV7())
			require.ErrorIs(t, writer.ConsumeEmploymentBasis(commandCtx, wrong), authz.ErrScopedCommandDenied)
			return writer.ConsumeEmploymentBasis(commandCtx, b)
		})
		require.NoError(t, err, subject)
		initial[subject] = b
		require.Equal(t, "owner", readAuthority(b)["authority_kind"])
		require.ErrorIs(t, escaped.ConsumeEmploymentBasis(escapedContext, b), authz.ErrScopedCommandDenied)
	}
	for _, subject := range []string{"synthetic-delegate-only", "synthetic-clinical", "user-admin"} {
		b := basis(subject)
		err := guard.WithGovernanceEmploymentActivation(ctx, req, b.StaffID, func(commandCtx context.Context, _ authz.GovernanceSourceEvidence, writer authz.EmploymentBasisWriter) error {
			return writer.ConsumeEmploymentBasis(commandCtx, b)
		})
		require.ErrorIs(t, err, authz.ErrScopedCommandDenied, subject)
	}
	t.Run("exact delegated source and revoked source with retained routing", func(t *testing.T) {
		issue := func(template string, epoch int64) uuid.UUID {
			id := uuid.Must(uuid.NewV7())
			raw, _ := json.Marshal(map[string]any{"subject": "user-reviewer", "expected_membership_epoch": epoch, "template_key": template, "scope_kind": "hospital", "scope_id": hospital, "reason": "Synthetic first-reviewer activation source", "valid_from": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
			w := governanceRequest(r, http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", string(raw))
			require.Equal(t, 202, w.Code, w.Body.String())
			return id
		}
		grant := issue("workforce.employment_activator.v1", 1)
		issue("workforce.person_preparer.v1", 2)
		delegated := req
		delegated.Principal.Subject = "user-reviewer"
		supplied := hospital
		delegated.Scope.ID = &supplied
		b := initial["synthetic-delegate"]
		b.GenerationID = uuid.Must(uuid.NewV7())
		b.EmploymentEpoch = 3
		err := guard.WithGovernanceEmploymentActivation(ctx, delegated, b.StaffID, func(commandCtx context.Context, source authz.GovernanceSourceEvidence, writer authz.EmploymentBasisWriter) error {
			supplied = uuid.Must(uuid.NewV7())
			require.Equal(t, authz.GovernanceGrantSource, source.Kind)
			require.Equal(t, grant, source.GrantID)
			require.Equal(t, hospital, *source.Scope.ID, "caller scope mutation cannot change the selected source scope")
			require.Equal(t, "workforce.employment_activator.v1", source.TemplateKey)
			return writer.ConsumeEmploymentBasis(commandCtx, b)
		})
		require.NoError(t, err)
		recorded := readAuthority(b)
		require.Equal(t, "grant", recorded["authority_kind"])
		require.Equal(t, grant.String(), recorded["source_grant_id"])
		require.EqualValues(t, 1, recorded["source_grant_version"])
		raw, _ := json.Marshal(map[string]any{"expected_version": 1, "decision": "revoke", "reason": "End the exact activation source while keeping unrelated governance routing"})
		revoked := governanceRequest(r, http.MethodPut, "/v1/iam/governance-grants/"+grant.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", string(raw))
		require.Equal(t, 202, revoked.Code, revoked.Body.String())
		delegated.Scope.ID = &hospital
		called := false
		err = guard.WithGovernanceEmploymentActivation(ctx, delegated, b.StaffID, func(context.Context, authz.GovernanceSourceEvidence, authz.EmploymentBasisWriter) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
		require.False(t, called, "a person-preparer source or retained coarse role cannot authorize employment activation")
	})

	sentinel := errors.New("synthetic product failure")
	b := initial["synthetic-ordinary"]
	b.GenerationID = uuid.Must(uuid.NewV7())
	b.EmploymentEpoch = 3
	err := guard.WithGovernanceEmploymentActivation(ctx, req, b.StaffID, func(commandCtx context.Context, _ authz.GovernanceSourceEvidence, writer authz.EmploymentBasisWriter) error {
		require.NoError(t, writer.ConsumeEmploymentBasis(commandCtx, b))
		return sentinel
	})
	require.ErrorIs(t, err, sentinel, "product failure must propagate through the shared transaction")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.iam_employment_basis','consume',true)`).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("iam_employment_bases").Where("tenant_id=? AND generation_id=?", testutil.TenantA, b.GenerationID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "a failed product transaction cannot retain its IAM basis")
		if err := tx.Table("iam_employment_basis_authorities").Where("tenant_id=? AND generation_id=?", testutil.TenantA, b.GenerationID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "a failed product transaction cannot retain source evidence for an uncommitted generation")

		var current struct {
			GenerationID    uuid.UUID
			EmploymentEpoch int64
		}
		if err := tx.Table("iam_employment_bases").Where("tenant_id=? AND hospital_id=? AND subject=?", testutil.TenantA, hospital, "synthetic-ordinary").Take(&current).Error; err != nil {
			return err
		}
		require.Equal(t, initial["synthetic-ordinary"].GenerationID, current.GenerationID)
		require.EqualValues(t, 2, current.EmploymentEpoch)

		return nil
	}))
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_governance_basis_recorder_disabled", SQL: "ALTER TABLE iam_employment_bases DISABLE TRIGGER iam_employment_basis_zz_authority"}}))
	defer func() {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_governance_basis_recorder_restored", SQL: "ALTER TABLE iam_employment_bases ENABLE TRIGGER iam_employment_basis_zz_authority"}}))
	}()
	called := false
	err = guard.WithGovernanceEmploymentActivation(ctx, req, b.StaffID, func(context.Context, authz.GovernanceSourceEvidence, authz.EmploymentBasisWriter) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "missing provenance support denies before the product callback")

}
