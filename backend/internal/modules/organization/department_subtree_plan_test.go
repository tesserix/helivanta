package organization_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestDepartmentSubtreeQueryUsesKeyedSeedAndParentIndex(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-PLAN-ROOT", uuid.Nil)
	child := s.node(t, "SYN-PLAN-CHILD", root)
	s.node(t, "SYN-PLAN-LEAF-A", child)
	s.node(t, "SYN-PLAN-LEAF-B", child)
	const subtreeSize, hospitalSize = 4, 5000
	require.NoError(t, s.db.WithTenant(tenantdb.WithActor(s.ctx, "user-manager"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO organization_departments(tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,effective_at,created_at,updated_at)
 SELECT ?::uuid,?::uuid,('01800000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01800000-0001-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'SYN-PEER-'||n,'department','Synthetic indexed peer',NULL,'active',1,
 transaction_timestamp(),transaction_timestamp(),transaction_timestamp() FROM generate_series(1,?) n`, testutil.TenantA, s.hospital, hospitalSize-subtreeSize).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,kind,name,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
 SELECT tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,0,'Synthetic plan fixture','user-manager',effective_at,created_at,created_at,updated_at
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND code LIKE 'SYN-PEER-%'`, testutil.TenantA, s.hospital).Error
	}))
	require.Eventually(t, func() bool {
		var ready bool
		err := s.db.WithTenant(s.ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT last_autoanalyze IS NOT NULL FROM pg_stat_user_tables WHERE schemaname='public' AND relname='organization_departments'`).Scan(&ready).Error
		})
		return err == nil && ready
	}, 90*time.Second, 250*time.Millisecond, "the plan fixture needs normal PostgreSQL auto-analysis; no elevated application role")
	type planNode struct {
		NodeType   string     `json:"Node Type"`
		Relation   string     `json:"Relation Name"`
		Index      string     `json:"Index Name"`
		ActualRows float64    `json:"Actual Rows"`
		Plans      []planNode `json:"Plans"`
	}
	var output []struct {
		Plan          planNode
		ExecutionTime float64 `json:"Execution Time"`
		PlanningTime  float64 `json:"Planning Time"`
	}
	require.NoError(t, s.db.WithTenant(s.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("organization_departments").Where("tenant_id=? AND hospital_id=?", testutil.TenantA, s.hospital).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, hospitalSize, count)
		var raw []byte
		if err := tx.Raw("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+organization.DepartmentSubtreeQueryForTest(), testutil.TenantA, s.hospital, "{"+root.String()+"}", testutil.TenantA, s.hospital, authz.DepartmentSubtreeLimit+1).Row().Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &output)
	}))
	require.Len(t, output, 1)
	var indexes []string
	var inspect func(planNode)
	inspect = func(node planNode) {
		if node.Relation == "organization_departments" {
			require.NotEqual(t, "Seq Scan", node.NodeType, "a department subtree read must never scan the hospital")
		}
		if node.Index != "" {
			indexes = append(indexes, node.Index)
		}
		if node.NodeType == "Recursive Union" {
			require.EqualValues(t, subtreeSize, node.ActualRows)
		}
		for _, child := range node.Plans {
			inspect(child)
		}
	}
	inspect(output[0].Plan)
	slices.Sort(indexes)
	require.Equal(t, []string{"organization_department_parent", "organization_departments_pkey"}, indexes, "one keyed seed and one parent-index descent")
	t.Logf("max-hospital fixture: %d nodes, subtree nodes=%d, planning=%.3fms execution=%.3fms, indexes=%v; not SLO evidence", hospitalSize, subtreeSize, output[0].PlanningTime, output[0].ExecutionTime, indexes)
}
