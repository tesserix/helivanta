//go:build workforce_perf

package workforce_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestSupervisionLongChainPlans(t *testing.T) {
	h := newSupervisionHarnessWithModule(t, legacySupervisionModule{Module: workforce.New()})
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status)
          SELECT ?::uuid,?::uuid,('019a1000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
          'SYN-LONG-'||lpad(n::text,4,'0'),'Synthetic long chain '||n::text,'other','active'
          FROM generate_series(1,2000) n`, testutil.TenantA, h.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_enrollment_profile(tenant_id,hospital_id,staff_id,engagement_type,supervisor_id)
          SELECT ?::uuid,?::uuid,('019a1000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'employee',
          CASE WHEN n<2000 THEN ('019a1000-0000-7000-8000-'||lpad(to_hex(n+1),12,'0'))::uuid END
          FROM generate_series(1,2000) n`, testutil.TenantA, h.hospital).Error
	}))
	var guard []tenantdb.Migration
	for _, migration := range workforce.New().Migrations() {
		if migration.ID == "0039_workforce_supervision" {
			guard = append(guard, migration)
		}
	}
	require.Len(t, guard, 1)
	require.NoError(t, h.db.Migrate(t.Context(), guard))
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		var definition string
		if err := tx.Raw(`SELECT pg_get_functiondef('workforce_assert_supervision_acyclic'::regproc)`).Scan(&definition).Error; err != nil {
			return err
		}
		start, end := strings.Index(definition, "WITH RECURSIVE"), strings.Index(definition, "INTO circular")
		require.GreaterOrEqual(t, start, 0)
		require.Greater(t, end, start)
		query := definition[start:end]
		for original, replacement := range map[string]string{"NEW.tenant_id": "$1::uuid", "NEW.hospital_id": "$2::uuid", "NEW.supervisor_id": "$3::uuid", "NEW.staff_id": "$4::uuid"} {
			query = strings.ReplaceAll(query, original, replacement)
		}
		var nested []byte
		if err := tx.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+query, testutil.TenantA, h.hospital, "019a1000-0000-7000-8000-000000000002", "019a1000-0000-7000-8000-000000000001").Scan(&nested); err != nil {
			return err
		}
		t.Logf("recursive owner plan: %s", nested)
		var ownerPlans []map[string]any
		if err := json.Unmarshal(nested, &ownerPlans); err != nil {
			return err
		}
		scans := 0
		var inspect func(map[string]any)
		inspect = func(node map[string]any) {
			if node["Node Type"] == "Recursive Union" {
				require.Equal(t, float64(1999), node["Actual Rows"])
			}
			if node["Relation Name"] == "workforce_enrollment_profile" {
				scans++
				require.Contains(t, node["Index Cond"], "staff_id", "each hop must seek the full staff key")
				require.LessOrEqual(t, node["Actual Rows"].(float64), float64(1))
			}
			if children, ok := node["Plans"].([]any); ok {
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
		}
		inspect(ownerPlans[0]["Plan"].(map[string]any))
		require.GreaterOrEqual(t, scans, 2)
		var raw []byte
		err := tx.Statement.ConnPool.QueryRowContext(t.Context(), `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) UPDATE workforce_enrollment_profile SET preferred_name=preferred_name WHERE tenant_id=$1 AND hospital_id=$2 AND staff_id=$3`, testutil.TenantA, h.hospital, "019a1000-0000-7000-8000-000000000001").Scan(&raw)
		if err != nil {
			return err
		}
		t.Logf("long-chain guard plan: %s", raw)
		var plans []map[string]any
		if err := json.Unmarshal(raw, &plans); err != nil {
			return err
		}
		require.Less(t, plans[0]["Execution Time"].(float64), float64(250), "the 2,000-person guard budget is 250 ms")
		return nil
	}))
	last := fmt.Sprintf("019a1000-0000-7000-8000-%012x", 2000)
	input := map[string]any{
		"expected_version": 1,
		"staff":            map[string]any{"employee_code": "SYN-LONG-2000", "display_name": "Synthetic long chain 2000", "job_category": "other", "employment_status": "active"},
		"profile":          map[string]any{"supervisor_id": "019a1000-0000-7000-8000-000000000001"},
	}
	started := time.Now()
	response := testutil.Do(h.router, "PUT", h.base+"/staff/"+last+"/enrollment", "admin", string(mustJSON(t, input)))
	elapsed := time.Since(started)
	t.Logf("2,000-person cycle refusal HTTP elapsed=%s", elapsed)
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"supervisor_cycle"`)
	require.Less(t, elapsed, time.Second, "the synthetic HTTP evaluation budget is one second")
}
