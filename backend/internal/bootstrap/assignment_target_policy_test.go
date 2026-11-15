package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
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

func assertAssignmentQualificationLocksAreReadOnly(t *testing.T, db *tenantdb.DB, parent context.Context, hospital, staff, link uuid.UUID) {
	t.Helper()
	t.Run("current owner qualification locks cannot mutate Staff or login links", func(t *testing.T) {
		now := time.Now()
		ctx, cancel := context.WithDeadline(tenantdb.WithActor(parent, "user-admin"), now.Add(5*time.Second))
		defer cancel()
		principal := authn.Principal{TenantID: testutil.TenantA, Subject: "user-admin", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}
		scope := authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "hospital", ID: &hospital}
		sources := iam.New(nil).GovernanceSources(db, platform.GovernanceTopology([]platform.Module{workforce.New()}, db))
		err := sources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: principal, Capability: authz.GovernanceAssignmentManage, Scope: scope}, func(held context.Context, source authz.GovernanceSourceEvidence) error {
			proof, err := authz.AssignmentTargetChoiceProof(authz.GovernedAssignmentSelectionRequest{Principal: principal, HospitalID: hospital, Capability: authz.GovernanceAssignmentManage, Source: source}, now)
			if err != nil {
				return err
			}
			deadline, _ := ctx.Deadline()
			proof["read_expires_at"] = deadline
			raw, err := json.Marshal(proof)
			if err != nil {
				return err
			}
			return db.WithTenantCommand(held, testutil.TenantA, func(tx *gorm.DB) (err error) {
				if err = tx.SavePoint("qualification_read_test").Error; err != nil {
					return err
				}
				defer func() { err = errors.Join(err, tx.RollbackTo("qualification_read_test").Error) }()
				if err = tx.Exec(`SELECT set_config('app.workforce_assignment_selection',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject',?,true),set_config('app.workforce_assignment_target_staff',?,true),set_config('app.workforce_assignment_target_generation','',true)`, string(raw), hospital.String(), principal.Subject, staff.String()).Error; err != nil {
					return err
				}
				for _, target := range []struct{ table, id string }{{"workforce_staff", staff.String()}, {"workforce_login_links", link.String()}} {
					var row struct{ Version int64 }
					require.NoError(t, tx.Raw("SELECT version FROM "+target.table+" WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE", testutil.TenantA, hospital, target.id).Take(&row).Error)
					require.Positive(t, row.Version)
					require.NoError(t, tx.SavePoint("qualification_write_test").Error)
					write := tx.Exec("UPDATE "+target.table+" SET version=version+1 WHERE tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, target.id)
					require.Error(t, write.Error, "a qualification read must not permit Staff or login-link mutation")
					require.NoError(t, tx.RollbackTo("qualification_write_test").Error)
					var unchanged struct{ Version int64 }
					require.NoError(t, tx.Raw("SELECT version FROM "+target.table+" WHERE tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, target.id).Take(&unchanged).Error)
					require.Equal(t, row.Version, unchanged.Version)
				}
				return nil
			})
		})
		require.NoError(t, err)
	})
}
