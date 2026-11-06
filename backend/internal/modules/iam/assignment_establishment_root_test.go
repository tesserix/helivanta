package iam_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const establishmentRootMigration = "0033_iam_establishment_root"

type beforeEstablishmentRoot struct{ *iam.Module }

func (m beforeEstablishmentRoot) Migrations() []tenantdb.Migration {
	all := m.Module.Migrations()
	for i, migration := range all {
		if migration.ID == establishmentRootMigration {
			return all[:i]
		}
	}
	panic("establishment root migration missing")
}

func requireConstraint(t *testing.T, err error, name string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "%v", err)
	require.Equal(t, name, pg.ConstraintName, pg.Message)
}

func TestEstablishmentRootStorageFailsClosed(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}, "reviewer": {"iam.assignment.review"}},
		Modules: []platform.Module{iam.New(nil)},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','tenant_admin'),(?,'user-target','staff_member')`,
			testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	const admitted = "workforce.leave_reviewer.v1"
	pharmacy, hospital := uuid.NewString(), uuid.NewString()
	typed := []string{"app.assignment_establishment_root", "typed"}
	departments := []string{"app.assignment_department_scope", "enabled"}
	session := func(actor string, settings []string, fn func(*gorm.DB) error) error {
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			settings = append([]string{"app.assignment_access", "authorized", "app.actor_subject", actor}, settings...)
			for i := 0; i < len(settings); i += 2 {
				if err := tx.Exec(`SELECT set_config(?,?,true)`, settings[i], settings[i+1]).Error; err != nil {
					return err
				}
			}
			return fn(tx)
		})
	}
	insert := func(tx *gorm.DB, root, template, establishment, scope string, department any) (string, error) {
		id := v7()
		return id, tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version,establishment_kind,scope_kind,department_id)
			VALUES(?,?,?,'user-target',?,'Synthetic establishment root',now(),now()+interval '1 hour','user-admin',?,'pending_scope',1,?,?,?)`,
			testutil.TenantA, id, root, template, v7(), establishment, scope, department).Error
	}
	count := func(settings []string, table, id string) int64 {
		var n int64
		require.NoError(t, session("user-admin", settings, func(tx *gorm.DB) error {
			return tx.Table(table).Where("id=?", id).Count(&n).Error
		}))
		return n
	}

	t.Run("root and scope kinds must agree", func(t *testing.T) {
		for _, c := range []struct {
			establishment, scope string
			department           any
		}{
			{"pharmacy", "hospital", nil}, {"pharmacy", "department", v7()}, {"pharmacy", "pharmacy", v7()},
			{"hospital", "pharmacy", nil}, {"clinic", "hospital", nil}, {"clinic", "clinic", nil},
		} {
			err := session("user-admin", append(typed, departments...), func(tx *gorm.DB) error {
				_, err := insert(tx, pharmacy, admitted, c.establishment, c.scope, c.department)
				return err
			})
			requireConstraint(t, err, "iam_assignment_proposals_scope_check")
		}
	})
	t.Run("every published template refuses a pharmacy root even when the session is typed", func(t *testing.T) {
		catalog := testutil.Do(r, http.MethodGet, "/v1/iam/staff-access-templates?catalog=staff-department-leave-reviews-v1", "admin", "")
		require.Equal(t, http.StatusOK, catalog.Code, catalog.Body.String())
		var definitions []iam.TemplateDefinition
		require.NoError(t, json.Unmarshal(catalog.Body.Bytes(), &definitions))
		require.Len(t, definitions, 15)
		for _, definition := range definitions {
			err := session("user-admin", append(typed, departments...), func(tx *gorm.DB) error {
				_, err := insert(tx, pharmacy, definition.Key, "pharmacy", "pharmacy", nil)
				return err
			})
			requireConstraint(t, err, "iam_assignment_proposals_template_establishment_check")
		}
		var kinds struct{ Known, Unknown string }
		require.NoError(t, session("user-admin", nil, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT array_to_string(iam_assignment_template_establishment_kinds(?),',') AS known,array_to_string(iam_assignment_template_establishment_kinds('workforce.unknown.v1'),',') AS unknown`, admitted).Scan(&kinds).Error
		}))
		require.Equal(t, "hospital", kinds.Known)
		require.Empty(t, kinds.Unknown, "an unknown template admits no root")
	})
	t.Run("sessions that have not declared the root cannot write a pharmacy row", func(t *testing.T) {
		err := session("user-admin", departments, func(tx *gorm.DB) error {
			_, err := insert(tx, pharmacy, admitted, "pharmacy", "pharmacy", nil)
			return err
		})
		require.ErrorContains(t, err, "row-level security")
	})

	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_admit_pharmacy_leave_reviewer", SQL: `CREATE OR REPLACE FUNCTION iam_assignment_template_establishment_kinds(key text) RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE key WHEN 'workforce.leave_reviewer.v1' THEN ARRAY['hospital','pharmacy']::text[] ELSE ARRAY['hospital']::text[] END
$$;`}}))
	var pharmacyRow, hospitalRow string
	require.NoError(t, session("user-admin", typed, func(tx *gorm.DB) error {
		var err error
		if pharmacyRow, err = insert(tx, pharmacy, admitted, "pharmacy", "pharmacy", nil); err != nil {
			return err
		}
		hospitalRow, err = insert(tx, hospital, admitted, "hospital", "hospital", nil)
		return err
	}), "the root and department policies are independent")

	t.Run("the root is immutable", func(t *testing.T) {
		for _, assignment := range []string{"establishment_kind='hospital',scope_kind='hospital'", "establishment_kind='clinic'"} {
			err := session("user-admin", append(typed, departments...), func(tx *gorm.DB) error {
				return tx.Exec(`UPDATE iam_assignment_proposals SET `+assignment+`,version=version+1 WHERE id=?`, pharmacyRow).Error
			})
			require.ErrorContains(t, err, "assignment establishment root is immutable")
		}
	})
	t.Run("an approved pharmacy root carries its kind into the snapshot", func(t *testing.T) {
		decisionRequest := v7()
		require.NoError(t, session("user-admin", typed, func(tx *gorm.DB) error {
			var request string
			if err := tx.Table("iam_assignment_proposals").Where("id=?", pharmacyRow).Pluck("request_id", &request).Error; err != nil {
				return err
			}
			steps := []struct {
				sql  string
				args []any
			}{
				{`SELECT set_config('app.assignment_scope_result',?,true)`, []any{request}},
				{`UPDATE iam_assignment_proposals SET status='pending_review',version=2 WHERE id=?`, []any{pharmacyRow}},
				{`SELECT set_config('app.assignment_command','review',true),set_config('app.actor_subject','user-reviewer',true)`, nil},
				{`INSERT INTO iam_assignment_decisions(tenant_id,id,proposal_id,request_id,expected_version,decision,reason,result_version,actor_subject) VALUES(?,?,?,?,2,'approve','Independent synthetic pharmacy review',3,'user-reviewer')`, []any{testutil.TenantA, v7(), pharmacyRow, decisionRequest}},
				{`UPDATE iam_assignment_proposals SET status='pending_decision_scope',version=3,request_id=? WHERE id=?`, []any{decisionRequest, pharmacyRow}},
				{`SELECT set_config('app.assignment_scope_result',?,true)`, []any{decisionRequest}},
				{`UPDATE iam_assignment_proposals SET status='approved_pending_activation',version=4 WHERE id=?`, []any{pharmacyRow}},
			}
			for _, step := range steps {
				if err := tx.Exec(step.sql, step.args...).Error; err != nil {
					return fmt.Errorf("%s: %w", step.sql, err)
				}
			}
			return nil
		}))
		var snapshot struct{ EstablishmentKind, ScopeKind string }
		require.NoError(t, session("user-admin", typed, func(tx *gorm.DB) error {
			return tx.Table("iam_assignment_snapshots").Select("establishment_kind,scope_kind").Where("id=?", pharmacyRow).Take(&snapshot).Error
		}))
		require.Equal(t, "pharmacy", snapshot.EstablishmentKind)
		require.Equal(t, "pharmacy", snapshot.ScopeKind)
	})
	t.Run("unaware paths never see a pharmacy root", func(t *testing.T) {
		for _, table := range []string{"iam_assignment_proposals", "iam_assignment_snapshots"} {
			require.Equal(t, int64(1), count(typed, table, pharmacyRow), table)
			require.Zero(t, count(departments, table, pharmacyRow), table)
		}
		require.Equal(t, int64(1), count(departments, "iam_assignment_proposals", hospitalRow))
		for _, path := range []string{"", "/decisions", "/history"} {
			w := testutil.Do(r, http.MethodGet, "/v1/iam/assignment-proposals/"+pharmacyRow+path, "admin", "")
			require.Equal(t, http.StatusNotFound, w.Code, path+" "+w.Body.String())
		}
		w := testutil.Do(r, http.MethodPost, "/v1/iam/assignment-proposals/"+pharmacyRow+"/decision", "admin", `{"expected_version":4,"decision":"withdraw","reason":"Synthetic withdrawal of a hidden root"}`)
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		list := testutil.Do(r, http.MethodGet, "/v1/iam/assignment-proposals", "admin", "")
		require.Equal(t, http.StatusOK, list.Code, list.Body.String())
		require.Contains(t, list.Body.String(), hospitalRow)
		require.NotContains(t, list.Body.String(), pharmacyRow)
		for _, table := range []string{"iam_assignment_proposals", "iam_assignment_snapshots"} {
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_drop_establishment_root_" + table, SQL: "DROP POLICY establishment_root ON " + table + ";"}}))
			require.Equal(t, int64(1), count(departments, table, pharmacyRow), "the hiding on %s must come from the restrictive policy", table)
		}
	})
}

// TestEstablishmentRootBackfillKeepsHospitalGrants proves 0033 is catalog-only for live hospital assignments.
func TestEstablishmentRootBackfillKeepsHospitalGrants(t *testing.T) {
	module := iam.New(nil)
	h := newGrantHarness(t, beforeEstablishmentRoot{module})
	department := v7()
	hospitalGrant := h.activate(t, "workforce.posting_reviewer.v1", "")
	departmentGrant := h.activate(t, departmentReviewTemplate, department)
	grants := func() []authz.ScopeGrant {
		grant, err := h.authority.AuthorizeScope(t.Context(), h.request())
		require.NoError(t, err)
		departments, err := h.authority.DepartmentScopes(t.Context(), h.request())
		require.NoError(t, err)
		return append([]authz.ScopeGrant{grant}, departments...)
	}
	before := grants()
	require.Len(t, before, 2)
	require.Equal(t, hospitalGrant, before[0].AssignmentID)
	require.Equal(t, departmentGrant, before[1].AssignmentID)

	const evidenceSQL = `SELECT
	 (SELECT jsonb_agg(to_jsonb(p)-'establishment_kind' ORDER BY p.id)::text FROM iam_assignment_proposals p WHERE p.id IN(?,?)) AS proposals,
	 (SELECT jsonb_agg(to_jsonb(s)-'establishment_kind' ORDER BY s.id,s.version)::text FROM iam_assignment_snapshots s WHERE s.id IN(?,?)) AS snapshots,
	 (SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id)::text FROM iam_assignment_decisions d WHERE d.proposal_id IN(?,?)) AS decisions,
	 (SELECT jsonb_agg(to_jsonb(x) ORDER BY x.id,x.version)::text FROM iam_assignment_proposal_history x WHERE x.id IN(?,?)) AS history,
	 (SELECT jsonb_agg(to_jsonb(e) ORDER BY e.hospital_id)::text FROM iam_assignment_scope_epochs e WHERE e.subject=?) AS scope_epochs,
	 (SELECT jsonb_agg(to_jsonb(m))::text FROM iam_subject_membership_epochs m WHERE m.subject=?) AS membership_epochs,
	 pg_relation_filenode('iam_assignment_proposals')::text||'/'||pg_relation_filenode('iam_assignment_snapshots')::text AS filenodes`
	type evidence struct{ Proposals, Snapshots, Decisions, History, ScopeEpochs, MembershipEpochs, Filenodes string }
	read := func() evidence {
		var out evidence
		h.session(t, func(tx *gorm.DB) error {
			return tx.Raw(evidenceSQL, hospitalGrant, departmentGrant, hospitalGrant, departmentGrant, hospitalGrant, departmentGrant, hospitalGrant, departmentGrant, departmentGrantSubject, departmentGrantSubject).Scan(&out).Error
		})
		return out
	}
	h.session(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-admin',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version)
			SELECT ?,overlay(gen_random_uuid()::text placing '7' from 15 for 1)::uuid,gen_random_uuid(),?,'workforce.leave_reviewer.v1','Synthetic neighbour '||n,now(),now()+interval '1 day','user-admin',gen_random_uuid(),'pending_scope',1
			FROM generate_series(1,100000) n`, testutil.TenantA, departmentGrantSubject).Error
	})
	baseline := read()
	require.NotEmpty(t, baseline.Decisions)
	require.NotEmpty(t, baseline.ScopeEpochs)

	started := time.Now()
	require.NoError(t, h.db.Migrate(t.Context(), module.Migrations()))
	t.Logf("0033 over 100,002 proposals took %s", time.Since(started))
	require.NoError(t, h.db.Migrate(t.Context(), module.Migrations()))

	require.Equal(t, baseline, read(), "versions, decisions, history, epochs and storage are unchanged")
	h.session(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_establishment_root','typed',true)`).Error; err != nil {
			return err
		}
		for _, table := range []string{"iam_assignment_proposals", "iam_assignment_snapshots"} {
			var kinds []string
			if err := tx.Table(table).Distinct("establishment_kind").Pluck("establishment_kind", &kinds).Error; err != nil {
				return err
			}
			require.Equal(t, []string{"hospital"}, kinds, table)
		}
		return nil
	})
	require.Equal(t, before, grants(), "the pre-migration OpenFGA tuples still authorize")
}
