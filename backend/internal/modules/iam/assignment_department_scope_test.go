package iam_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const departmentReviewTemplate = "workforce.department_posting_reviewer.v1"

func TestDepartmentAssignmentStorageFailsClosed(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga,
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/v1/iam/members", strings.NewReader(`{"subject":"user-target","role_key":"staff_member"}`))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, req)
	require.Equal(t, 202, w.Code, w.Body.String())

	hospital := uuid.NewString()
	department := uuid.Must(uuid.NewV7()).String()
	session := func(departmentScope bool, fn func(*gorm.DB) error) error {
		return db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			settings := []string{"app.assignment_access", "authorized", "app.actor_subject", "user-admin"}
			if departmentScope {
				settings = append(settings, "app.assignment_department_scope", "enabled")
			}
			for i := 0; i < len(settings); i += 2 {
				if err := tx.Exec(`SELECT set_config(?,?,true)`, settings[i], settings[i+1]).Error; err != nil {
					return err
				}
			}
			return fn(tx)
		})
	}
	insert := func(tx *gorm.DB, template, scope string, departmentID any) (string, error) {
		id := uuid.Must(uuid.NewV7()).String()
		return id, tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version,scope_kind,department_id)
			VALUES(?,?,?,'user-target',?,'Synthetic department scope',now(),now()+interval '1 hour','user-admin',?,'pending_scope',1,?,?)`,
			testutil.TenantA, id, hospital, template, uuid.Must(uuid.NewV7()).String(), scope, departmentID).Error
	}

	t.Run("scope kind and department must agree", func(t *testing.T) {
		for _, c := range []struct {
			scope      string
			department any
		}{{"department", nil}, {"hospital", department}, {"department", uuid.NewString()}, {"department", hospital}, {"ward", department}} {
			err := session(true, func(tx *gorm.DB) error {
				_, err := insert(tx, departmentReviewTemplate, c.scope, c.department)
				return err
			})
			requireConstraint(t, err, "iam_assignment_proposals_scope_check")
		}
	})
	t.Run("template must allow the scope kind", func(t *testing.T) {
		err := session(true, func(tx *gorm.DB) error {
			_, err := insert(tx, "workforce.posting_reviewer.v1", "department", department)
			return err
		})
		requireConstraint(t, err, "iam_assignment_proposals_template_scope_check")
		err = session(true, func(tx *gorm.DB) error {
			_, err := insert(tx, departmentReviewTemplate, "hospital", nil)
			return err
		})
		requireConstraint(t, err, "iam_assignment_proposals_template_scope_check")
	})
	t.Run("unaware sessions cannot write department rows", func(t *testing.T) {
		err := session(false, func(tx *gorm.DB) error {
			_, err := insert(tx, departmentReviewTemplate, "department", department)
			return err
		})
		require.ErrorContains(t, err, "row-level security")
	})

	var departmentRow, hospitalRow string
	require.NoError(t, session(true, func(tx *gorm.DB) error {
		var err error
		if departmentRow, err = insert(tx, departmentReviewTemplate, "department", department); err != nil {
			return err
		}
		hospitalRow, err = insert(tx, "workforce.posting_reviewer.v1", "hospital", nil)
		return err
	}))
	visible := func(departmentScope bool) []string {
		var ids []string
		require.NoError(t, session(departmentScope, func(tx *gorm.DB) error {
			return tx.Table("iam_assignment_proposals").Where("id IN ?", []string{departmentRow, hospitalRow}).Order("id").Pluck("id", &ids).Error
		}))
		return ids
	}

	t.Run("scope is immutable after insert", func(t *testing.T) {
		for _, assignment := range []string{
			"department_id='" + uuid.Must(uuid.NewV7()).String() + "'",
			"scope_kind='hospital',department_id=NULL",
		} {
			err := session(true, func(tx *gorm.DB) error {
				return tx.Exec(`UPDATE iam_assignment_proposals SET `+assignment+`,version=version+1 WHERE id=?`, departmentRow).Error
			})
			require.ErrorContains(t, err, "immutable proposal or stale version")
		}
	})
	t.Run("department proposals need both owners before review", func(t *testing.T) {
		advance := func(answers ...string) error {
			return session(true, func(tx *gorm.DB) error {
				var request string
				if err := tx.Table("iam_assignment_proposals").Where("id=?", departmentRow).Pluck("request_id", &request).Error; err != nil {
					return err
				}
				if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, request).Error; err != nil {
					return err
				}
				for _, owner := range answers {
					if err := tx.Exec(`INSERT INTO iam_assignment_department_verifications(tenant_id,proposal_id,request_id,owner,eligible) VALUES(?,?,?,?,true)`, testutil.TenantA, departmentRow, request, owner).Error; err != nil {
						return err
					}
				}
				return tx.Exec(`UPDATE iam_assignment_proposals SET status='pending_review',version=version+1 WHERE id=?`, departmentRow).Error
			})
		}
		require.ErrorContains(t, advance(), "both scope owners must confirm the department")
		require.ErrorContains(t, advance("workforce"), "both scope owners must confirm the department")
		require.ErrorContains(t, advance("organization"), "both scope owners must confirm the department")
		require.NoError(t, advance("workforce", "organization"))
		err := session(true, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE iam_assignment_department_verifications SET eligible=false WHERE proposal_id=?`, departmentRow).Error
		})
		require.Error(t, err, "owner answers are append-only")
		err = session(true, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, uuid.Must(uuid.NewV7()).String()).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO iam_assignment_department_verifications(tenant_id,proposal_id,request_id,owner,eligible) SELECT tenant_id,id,request_id,'organization',true FROM iam_assignment_proposals WHERE id=?`, departmentRow).Error
		})
		require.ErrorContains(t, err, "current department proposal request required")
	})

	t.Run("unaware sessions see no department rows", func(t *testing.T) {
		require.ElementsMatch(t, []string{departmentRow, hospitalRow}, visible(true))
		require.Equal(t, []string{hospitalRow}, visible(false))
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_drop_department_scope_policy", SQL: "DROP POLICY department_scope ON iam_assignment_proposals;"}}))
		require.ElementsMatch(t, []string{departmentRow, hospitalRow}, visible(false), "the hiding must come from the restrictive policy")
	})
}
