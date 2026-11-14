package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func exerciseNativeOperatingGovernance(t *testing.T, router *gin.Engine, db *tenantdb.DB, ctx context.Context, kind string, employer, staff, link uuid.UUID) {
	t.Helper()
	call := func(method, path, actor string, input any, status int) string {
		t.Helper()
		raw := ""
		if input != nil {
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			raw = string(encoded)
		}
		response := governanceRequest(router, method, path, actor, raw)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}
	location := uuid.Must(uuid.NewV7())
	template := "pharmacy.inventory_receiver.v1"
	if kind == "pharmacy" {
		call(http.MethodPut, "/v1/pharmacy/pharmacies/"+employer.String()+"/outlets/"+location.String(), "admin", map[string]any{"code": "SYN-NATIVE-OUTLET", "name": "Synthetic native duty outlet", "country_code": "IN", "timezone": "Asia/Kolkata"}, http.StatusOK)
	} else {
		template = "supply.inventory_receiver.v1"
		call(http.MethodPut, "/v1/supply/parties/"+employer.String()+"/premises/"+location.String(), "admin", map[string]any{"expected_version": 0, "code": "SYN-NATIVE-WAREHOUSE", "name": "Synthetic native duty warehouse", "kind": "warehouse", "line1": "Synthetic test building", "line2": "", "city": "Bhubaneswar", "region": "IN-OD", "postal_code": "751001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": "active"}, http.StatusOK)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := iam.GovernanceGrantInput{Subject: "user-auditor", ExpectedMembershipEpoch: 1, TemplateKey: "iam.operating_access_reviewer.v1", ScopeKind: kind, ScopeID: &employer, Reason: "Synthetic independent duty reviewer", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	grantID := uuid.Must(uuid.NewV7())
	grantPath := "/v1/iam/governance-grants/" + grantID.String()
	call(http.MethodPut, grantPath, "admin", grant, http.StatusAccepted)
	call(http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.GovernanceGrantDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "Independent owner approves narrow operating source"}, http.StatusAccepted)
	input := iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: kind, EmployerID: employer, LocationID: location, ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}, Subject: "user-reviewer", TemplateKey: template, Reason: "Synthetic native staff receiving duty", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	prefix := "/v1/iam/operating-governance-employers/" + kind + "/" + employer.String()
	duty := uuid.Must(uuid.NewV7())
	path := prefix + "/duties/" + duty.String()

	prepared := call(http.MethodPut, path, "owner2", input, http.StatusCreated)
	func() {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_native_operating_disable_origin_recorder", SQL: "ALTER TABLE iam_operating_duties DISABLE TRIGGER iam_operating_native_record"}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_native_operating_restore_origin_recorder", SQL: "ALTER TABLE iam_operating_duties ENABLE TRIGGER iam_operating_native_record"}}))
		}()
		failed := uuid.Must(uuid.NewV7())
		call(http.MethodPut, prefix+"/duties/"+failed.String(), "owner2", input, http.StatusServiceUnavailable)
		var original struct{ Authority json.RawMessage }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_operating_governance_origins").Select("authority").Where("tenant_id=? AND duty_id=?", testutil.TenantA, duty).Take(&original).Error
		}))
		lateID := uuid.Must(uuid.NewV7())
		body, err := json.Marshal(input)
		require.NoError(t, err)
		lateErr := db.WithTenant(tenantdb.WithActor(ctx, "user-owner2"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_governance_access','authorized',true),set_config('app.iam_operating_command','prepare',true),set_config('app.iam_operating_native_authority',?,true),set_config('app.iam_operating_native_kind',?,true),set_config('app.iam_operating_native_employer',?,true),set_config('app.iam_operating_native_duty',?,true),set_config('app.iam_operating_native_target',?,true)`, string(original.Authority), kind, employer.String(), lateID.String(), input.Subject).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO iam_operating_duties(tenant_id,id,employer_kind,employer_id,location_id,subject,template_key,membership_epoch,input,prepared_by,status,version,authority_epoch,actor_subject,reason,created_at,updated_at)VALUES(?,?,?,?,?,?,?,1,?::jsonb,?,'pending_review',1,1,?,?,clock_timestamp(),clock_timestamp())`, testutil.TenantA, lateID, kind, employer, location, input.Subject, input.TemplateKey, string(body), "user-owner2", "user-owner2", input.Reason).Error
		})
		var latePG *pgconn.PgError
		require.ErrorAs(t, lateErr, &latePG)
		require.Equal(t, "23514", latePG.Code)
		require.Equal(t, "iam_operating_native_completion", latePG.ConstraintName)

		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec("SELECT set_config('app.iam_operating_access','authorized',true)").Error)
			for _, rejected := range []uuid.UUID{failed, lateID} {
				for _, table := range []string{"iam_operating_duties", "iam_operating_duty_history", "iam_operating_governance_origins"} {
					key := "duty_id"
					if table == "iam_operating_duties" {
						key = "id"
					}
					var count int64
					require.NoError(t, tx.Table(table).Where("tenant_id=? AND "+key+"=?", testutil.TenantA, rejected).Count(&count).Error)
					require.Zero(t, count)
				}
			}
			return nil
		}))
	}()

	require.NotContains(t, prepared, "owner_membership_id")
	require.NotContains(t, prepared, "source_grant_id")
	require.JSONEq(t, prepared, call(http.MethodPut, path, "owner2", input, http.StatusOK))
	require.JSONEq(t, prepared, call(http.MethodGet, path+"/proposal-receipt", "owner2", nil, http.StatusOK))
	require.Contains(t, call(http.MethodGet, prefix+"/staff/"+staff.String()+"/duties", "auditor", nil, http.StatusOK), duty.String())
	approval := iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "Independent reviewer verifies current employer and Staff"}
	call(http.MethodPut, path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner2", approval, http.StatusConflict)
	command := uuid.Must(uuid.NewV7())
	commandPath := path + "/commands/" + command.String()

	func() {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_native_operating_disable_decision_recorder", SQL: "ALTER TABLE iam_operating_commands DISABLE TRIGGER iam_operating_native_record"}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_native_operating_restore_decision_recorder", SQL: "ALTER TABLE iam_operating_commands ENABLE TRIGGER iam_operating_native_record"}}))
		}()
		call(http.MethodPut, commandPath, "auditor", approval, http.StatusServiceUnavailable)
	}()

	reviewed := call(http.MethodPut, commandPath, "auditor", approval, http.StatusCreated)
	require.Contains(t, reviewed, "approved_pending_activation")
	require.JSONEq(t, reviewed, call(http.MethodPut, commandPath, "auditor", approval, http.StatusOK))
	require.JSONEq(t, reviewed, call(http.MethodGet, commandPath, "auditor", nil, http.StatusOK))
	call(http.MethodGet, "/v1/iam/operating-duties/"+duty.String(), "admin", nil, http.StatusForbidden)
	legacyErr := db.WithTenant(tenantdb.WithActor(ctx, "user-admin"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_operating_command','revoke',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE iam_operating_duties SET status='revoked',version=version+1,authority_epoch=authority_epoch+1,actor_subject='user-admin',reason='A legacy command must not mutate a native duty'WHERE tenant_id=? AND id=?`, testutil.TenantA, duty).Error
	})
	var pg *pgconn.PgError
	require.ErrorAs(t, legacyErr, &pg)
	require.Equal(t, "42501", pg.Code)

	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("SELECT set_config('app.iam_operating_access','authorized',true)").Error)
		var origins, decisions, managers int64
		require.NoError(t, tx.Table("iam_operating_governance_origins").Where("tenant_id=? AND duty_id=?", testutil.TenantA, duty).Count(&origins).Error)
		require.EqualValues(t, 1, origins)
		require.NoError(t, tx.Table("iam_operating_governance_commands").Where("tenant_id=? AND command_id=?", testutil.TenantA, command).Count(&decisions).Error)
		require.EqualValues(t, 1, decisions)
		require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject IN('user-owner2','user-auditor')", testutil.TenantA).Count(&managers).Error)
		require.Zero(t, managers)
		return nil
	}))
	call(http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", iam.GovernanceGrantDecisionInput{ExpectedVersion: 2, Decision: "revoke", Reason: "Synthetic native source cutoff"}, http.StatusAccepted)
	response := governanceRequest(router, http.MethodGet, commandPath, "auditor", "")
	require.True(t, response.Code == http.StatusNotFound || response.Code == http.StatusForbidden, response.Body.String())
	replacement := uuid.Must(uuid.NewV7())
	replacementPath := "/v1/iam/governance-grants/" + replacement.String()
	call(http.MethodPut, replacementPath, "admin", grant, http.StatusAccepted)
	call(http.MethodPut, replacementPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.GovernanceGrantDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "New source must not adopt former command receipts"}, http.StatusAccepted)
	call(http.MethodGet, commandPath, "auditor", nil, http.StatusNotFound)
	call(http.MethodPut, commandPath, "auditor", approval, http.StatusNotFound)
}
