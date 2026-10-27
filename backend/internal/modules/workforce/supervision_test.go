package workforce_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestEnrollmentRejectsTwoPersonSupervisionCycle(t *testing.T) {
	h := newSupervisionHarness(t)
	router, base := h.router, h.base
	first, second := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	firstInput := map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": "SYN-SUP-FIRST", "display_name": "Synthetic First", "job_category": "other", "employment_status": "active"},
		"profile":          map[string]any{},
	}
	secondInput := map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": "SYN-SUP-SECOND", "display_name": "Synthetic Second", "job_category": "other", "employment_status": "active"},
		"profile":          map[string]any{},
	}
	firstPath, secondPath := base+"/staff/"+first+"/enrollment", base+"/staff/"+second+"/enrollment"
	for path, body := range map[string]map[string]any{firstPath: firstInput, secondPath: secondInput} {
		response := testutil.Do(router, "PUT", path, "admin", string(mustJSON(t, body)))
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	firstInput["expected_version"] = 1
	firstInput["profile"] = map[string]any{"supervisor_id": second}
	response := testutil.Do(router, "PUT", firstPath, "admin", string(mustJSON(t, firstInput)))
	require.Equal(t, 200, response.Code, response.Body.String())

	secondInput["expected_version"] = 1
	secondInput["profile"] = map[string]any{"supervisor_id": first}
	response = testutil.Do(router, "PUT", secondPath, "admin", string(mustJSON(t, secondInput)))
	require.Equal(t, 409, response.Code, response.Body.String())
	var refusal map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refusal))
	require.Equal(t, "supervisor_cycle", refusal["error"])
	require.NotContains(t, response.Body.String(), first)
	require.NotContains(t, response.Body.String(), "Synthetic First")

	response = testutil.Do(router, "GET", secondPath, "admin", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var retained struct {
		Version int64 `json:"version"`
		Profile struct {
			SupervisorID string `json:"supervisor_id"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &retained))
	require.EqualValues(t, 1, retained.Version)
	require.Empty(t, retained.Profile.SupervisorID)
}

type supervisionHarness struct {
	router   *gin.Engine
	db       *tenantdb.DB
	hospital string
	base     string
}

func newSupervisionHarness(t *testing.T) *supervisionHarness {
	t.Helper()
	return newSupervisionHarnessWithModule(t, workforce.New())
}

func newSupervisionHarnessWithModule(t *testing.T, module platform.Module) *supervisionHarness {
	t.Helper()
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}},
		Modules: []platform.Module{module},
	})
	hospital := uuid.Must(uuid.NewV7()).String()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(router, "PUT", base, "admin", `{"name":"Synthetic Supervision Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(router, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)

	return &supervisionHarness{router: router, db: db, hospital: hospital, base: base}
}

func (h *supervisionHarness) otherHospital(t *testing.T) *supervisionHarness {
	t.Helper()
	hospital := uuid.Must(uuid.NewV7()).String()
	base := "/v1/workforce/hospitals/" + hospital
	response := testutil.Do(h.router, "PUT", base, "admin", `{"name":"Synthetic other supervision hospital"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(h.router, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	return &supervisionHarness{router: h.router, db: h.db, hospital: hospital, base: base}
}

func (h *supervisionHarness) person(t *testing.T, code string) (string, string, map[string]any) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	path := h.base + "/staff/" + id + "/enrollment"
	input := map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "other", "employment_status": "active"},
		"profile":          map[string]any{},
	}
	response := testutil.Do(h.router, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 200, response.Code, response.Body.String())
	input["expected_version"] = 1
	return id, path, input
}

func (h *supervisionHarness) scoped(t *testing.T, fn func(*gorm.DB) error) error {
	t.Helper()
	return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, h.hospital).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func TestEnrollmentRejectsIndirectSupervisionCycleAndAllowsRepair(t *testing.T) {
	h := newSupervisionHarness(t)
	first, firstPath, firstInput := h.person(t, "SYN-CHAIN-FIRST")
	second, secondPath, secondInput := h.person(t, "SYN-CHAIN-SECOND")
	third, thirdPath, thirdInput := h.person(t, "SYN-CHAIN-THIRD")
	firstInput["profile"] = map[string]any{"supervisor_id": second}
	response := testutil.Do(h.router, "PUT", firstPath, "admin", string(mustJSON(t, firstInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
	secondInput["profile"] = map[string]any{"supervisor_id": third}
	response = testutil.Do(h.router, "PUT", secondPath, "admin", string(mustJSON(t, secondInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
	thirdInput["profile"] = map[string]any{"supervisor_id": first}
	response = testutil.Do(h.router, "PUT", thirdPath, "admin", string(mustJSON(t, thirdInput)))
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"supervisor_cycle"`)

	secondInput["expected_version"] = 2
	secondInput["profile"] = map[string]any{}
	response = testutil.Do(h.router, "PUT", secondPath, "admin", string(mustJSON(t, secondInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(h.router, "PUT", thirdPath, "admin", string(mustJSON(t, thirdInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
}

func TestEnrollmentSerializesConcurrentSupervisionChanges(t *testing.T) {
	h := newSupervisionHarness(t)
	first, firstPath, firstInput := h.person(t, "SYN-RACE-FIRST")
	second, secondPath, secondInput := h.person(t, "SYN-RACE-SECOND")
	firstInput["profile"] = map[string]any{"supervisor_id": second}
	secondInput["profile"] = map[string]any{"supervisor_id": first}
	paths := []string{firstPath, secondPath}
	bodies := []string{string(mustJSON(t, firstInput)), string(mustJSON(t, secondInput))}
	codes := make([]int, 2)
	replies := make([]string, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			response := testutil.Do(h.router, "PUT", paths[i], "admin", bodies[i])
			codes[i], replies[i] = response.Code, response.Body.String()
		}()
	}
	close(start)
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes, replies)
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND action='workforce.enrollment.changed'", testutil.TenantA, h.hospital).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, count, "the refused write must not publish a success audit")
		if err := tx.Table("outbox_events").Where("subject=?", workforce.EnrollmentChanged).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, count, "the refused write must not publish an enrollment event")
		return nil
	}))
}

func TestSupervisionDatabaseConstraintRejectsDirectCycleWrite(t *testing.T) {
	h := newSupervisionHarness(t)
	first, firstPath, firstInput := h.person(t, "SYN-SQL-FIRST")
	second, secondPath, _ := h.person(t, "SYN-SQL-SECOND")
	firstInput["profile"] = map[string]any{"supervisor_id": second}
	response := testutil.Do(h.router, "PUT", firstPath, "admin", string(mustJSON(t, firstInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
	err := h.scoped(t, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_enrollment_profile SET supervisor_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`, first, testutil.TenantA, h.hospital, second).Error
	})
	var refusal *pgconn.PgError
	require.True(t, errors.As(err, &refusal), err)
	require.Equal(t, "23514", refusal.Code)
	require.Equal(t, "workforce_enrollment_supervisor_acyclic", refusal.ConstraintName)
	response = testutil.Do(h.router, "GET", secondPath, "admin", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var retained struct {
		Profile struct {
			SupervisorID string `json:"supervisor_id"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &retained))
	require.Empty(t, retained.Profile.SupervisorID)
}

type legacySupervisionModule struct{ platform.Module }

func (m legacySupervisionModule) Migrations() []tenantdb.Migration {
	migrations := m.Module.Migrations()
	legacy := make([]tenantdb.Migration, 0, len(migrations))
	for _, migration := range migrations {
		if migration.ID != "0039_workforce_supervision" {
			legacy = append(legacy, migration)
		}
	}
	return legacy
}

func TestSupervisionMigrationAllowsRepairOfExistingCycle(t *testing.T) {
	h := newSupervisionHarnessWithModule(t, legacySupervisionModule{Module: workforce.New()})
	first, second := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-LEGACY-FIRST','Synthetic legacy first','other','active'),(?,?,?,'SYN-LEGACY-SECOND','Synthetic legacy second','other','active')`, testutil.TenantA, h.hospital, first, testutil.TenantA, h.hospital, second).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_enrollment_profile(tenant_id,hospital_id,staff_id,engagement_type,supervisor_id) VALUES(?,?,?,'employee',?),(?,?,?,'employee',?)`, testutil.TenantA, h.hospital, first, second, testutil.TenantA, h.hospital, second, first).Error
	}))
	var guard []tenantdb.Migration
	for _, migration := range workforce.New().Migrations() {
		if migration.ID == "0039_workforce_supervision" {
			guard = append(guard, migration)
		}
	}
	require.Len(t, guard, 1)
	require.NoError(t, h.db.Migrate(t.Context(), guard))
	_, thirdPath, thirdInput := h.person(t, "SYN-LEGACY-THIRD")
	thirdInput["profile"] = map[string]any{"supervisor_id": first}
	response := testutil.Do(h.router, "PUT", thirdPath, "admin", string(mustJSON(t, thirdInput)))
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"supervisor_cycle"`)

	repair := map[string]any{
		"expected_version": 1,
		"staff":            map[string]any{"employee_code": "SYN-LEGACY-FIRST", "display_name": "Synthetic legacy first", "job_category": "other", "employment_status": "active"},
		"profile":          map[string]any{},
	}
	response = testutil.Do(h.router, "PUT", h.base+"/staff/"+first+"/enrollment", "admin", string(mustJSON(t, repair)))
	require.Equal(t, 200, response.Code, response.Body.String())
	response = testutil.Do(h.router, "PUT", thirdPath, "admin", string(mustJSON(t, thirdInput)))
	require.Equal(t, 200, response.Code, response.Body.String())
}

func TestEnrollmentReturnsUnavailableWhenSupervisionLockIsBusy(t *testing.T) {
	h := newSupervisionHarness(t)
	_, path, input := h.person(t, "SYN-LOCK-TARGET")
	input["profile"] = map[string]any{"preferred_name": "Synthetic pending edit"}
	locked := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan struct{})
	var holderErr error
	go func() {
		defer close(holderDone)
		holderErr = h.scoped(t, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, testutil.TenantA, h.hospital).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	// Release before testing cancels t.Context during cleanup.
	defer func() {
		close(release)
		<-holderDone
		require.NoError(t, holderErr)
	}()
	select {
	case <-locked:
	case <-holderDone:
		t.Fatalf("lock holder failed: %v", holderErr)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	other := h.otherHospital(t)
	_, otherPath, otherInput := other.person(t, "SYN-OTHER-HOSPITAL")
	otherInput["profile"] = map[string]any{"preferred_name": "Synthetic independent hospital edit"}
	independent := testutil.Do(h.router, "PUT", otherPath, "admin", string(mustJSON(t, otherInput)))
	require.Equal(t, 200, independent.Code, independent.Body.String())
	response := testutil.Do(h.router, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"supervision_busy"`)
	response = testutil.Do(h.router, "GET", path, "admin", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var retained struct {
		Version int64 `json:"version"`
		Profile struct {
			PreferredName string `json:"preferred_name"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &retained))
	require.EqualValues(t, 1, retained.Version)
	require.Empty(t, retained.Profile.PreferredName)
}

func TestEnrollmentDoesNotResolveForeignHospitalSupervisor(t *testing.T) {
	h := newSupervisionHarness(t)
	_, path, input := h.person(t, "SYN-LOCAL-SCOPE")
	foreign, _, _ := h.otherHospital(t).person(t, "SYN-FOREIGN-SCOPE")
	input["profile"] = map[string]any{"supervisor_id": foreign}
	response := testutil.Do(h.router, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), foreign)
	require.NotContains(t, response.Body.String(), "SYN-FOREIGN-SCOPE")
	response = testutil.Do(h.router, "GET", path, "admin", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var retained struct {
		Version int64 `json:"version"`
		Profile struct {
			SupervisorID string `json:"supervisor_id"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &retained))
	require.EqualValues(t, 1, retained.Version)
	require.Empty(t, retained.Profile.SupervisorID)
}

func TestSupervisionConstraintFailsClosedForRepeatableReadSnapshots(t *testing.T) {
	h := newSupervisionHarness(t)
	first, _, _ := h.person(t, "SYN-SNAPSHOT-FIRST")
	second, _, _ := h.person(t, "SYN-SNAPSHOT-SECOND")
	var database string
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error { return tx.Raw("SELECT current_database()").Scan(&database).Error }))
	appDSN, _, _ := testinfra.StartPostgres(t)
	parsed, err := url.Parse(appDSN)
	require.NoError(t, err)
	parsed.Path = "/" + database
	query := parsed.Query()
	query.Set("default_transaction_isolation", "repeatable read")
	parsed.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	runtime, err := tenantdb.OpenRuntime(t.Context(), parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	ready := make(chan struct{}, 2)
	write := make(chan struct{})
	results := make([]error, 2)
	people := []string{first, second}
	var wg sync.WaitGroup
	for i := range people {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reported := false
			defer func() {
				if !reported {
					ready <- struct{}{}
				}
			}()
			results[i] = runtime.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, h.hospital).Error; err != nil {
					return err
				}
				var isolation string
				if err := tx.Raw("SHOW transaction_isolation").Scan(&isolation).Error; err != nil {
					return err
				}
				if isolation != "repeatable read" {
					return errors.New("snapshot fixture did not use repeatable read")
				}
				var count int64
				if err := tx.Table("workforce_enrollment_profile").Where("tenant_id=? AND hospital_id=?", testutil.TenantA, h.hospital).Count(&count).Error; err != nil {
					return err
				}
				ready <- struct{}{}
				reported = true
				<-write
				return tx.Exec(`UPDATE workforce_enrollment_profile SET supervisor_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`, people[1-i], testutil.TenantA, h.hospital, people[i]).Error
			})
		}()
	}
	<-ready
	<-ready
	close(write)
	wg.Wait()
	var cycles int64
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM workforce_enrollment_profile p JOIN workforce_enrollment_profile q ON (q.tenant_id,q.hospital_id,q.staff_id)=(p.tenant_id,p.hospital_id,p.supervisor_id) WHERE p.tenant_id=? AND p.hospital_id=? AND q.supervisor_id=p.staff_id`, testutil.TenantA, h.hospital).Scan(&cycles).Error
	}))
	require.Zero(t, cycles, "stale database snapshots must not commit a reporting cycle; writes: %v", results)
	for _, err := range results {
		var refusal *pgconn.PgError
		require.True(t, errors.As(err, &refusal), err)
		require.Equal(t, "25001", refusal.Code)
		require.Equal(t, "workforce_supervision_isolation", refusal.ConstraintName)
	}
}

func TestSupervisionConstraintRefusesLostReadAuthorityAtDeferredCommit(t *testing.T) {
	for _, loss := range []string{"read_context", "hospital_scope", "tenant_scope", "hospital_grant", "hospital_reader"} {
		t.Run(loss, func(t *testing.T) {
			h := newSupervisionHarness(t)
			first, firstPath, firstInput := h.person(t, "SYN-CONTEXT-FIRST")
			second, secondPath, _ := h.person(t, "SYN-CONTEXT-SECOND")
			firstInput["profile"] = map[string]any{"supervisor_id": second}
			response := testutil.Do(h.router, "PUT", firstPath, "admin", string(mustJSON(t, firstInput)))
			require.Equal(t, 200, response.Code, response.Body.String())
			err := h.scoped(t, func(tx *gorm.DB) error {
				if err := tx.Exec(`SET CONSTRAINTS workforce_enrollment_supervisor_acyclic DEFERRED`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`UPDATE workforce_enrollment_profile SET supervisor_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`, first, testutil.TenantA, h.hospital, second).Error; err != nil {
					return err
				}
				switch loss {
				case "read_context":
					return tx.Exec(`SELECT set_config('app.workforce_enrollment','',true)`).Error
				case "hospital_scope":
					return tx.Exec(`SELECT set_config('app.workforce_hospital','',true)`).Error
				case "tenant_scope":
					return tx.Exec(`SELECT set_config('app.tenant_id','',true)`).Error
				case "hospital_reader":
					return tx.Exec(`UPDATE workforce_hospital_access SET level='reader' WHERE tenant_id=? AND hospital_id=? AND subject='user-admin'`, testutil.TenantA, h.hospital).Error
				default:
					return tx.Exec(`DELETE FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-admin'`, testutil.TenantA, h.hospital).Error
				}
			})
			var refusal *pgconn.PgError
			require.True(t, errors.As(err, &refusal), "lost read authority must not turn hidden ancestors into a terminal supervisor: %v", err)
			require.Equal(t, "42501", refusal.Code)
			require.Equal(t, "workforce_supervision_authority", refusal.ConstraintName)
			response = testutil.Do(h.router, "GET", secondPath, "admin", "")
			require.Equal(t, 200, response.Code, response.Body.String())
			var retained struct {
				Version int64 `json:"version"`
				Profile struct {
					SupervisorID string `json:"supervisor_id"`
				} `json:"profile"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &retained))
			require.EqualValues(t, 1, retained.Version)
			require.Empty(t, retained.Profile.SupervisorID)
		})
	}
}

func TestEnrollmentAcceptsDirectoryOnlySupervisor(t *testing.T) {
	h := newSupervisionHarness(t)
	_, path, input := h.person(t, "SYN-DIRECTORY-CHILD")
	supervisor := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-DIRECTORY-SUPERVISOR','Synthetic directory supervisor','other','active')`, testutil.TenantA, h.hospital, supervisor).Error
	}))
	input["profile"] = map[string]any{"supervisor_id": supervisor}
	response := testutil.Do(h.router, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 200, response.Code, response.Body.String())
	var saved struct {
		Version int64 `json:"version"`
		Profile struct {
			SupervisorID string `json:"supervisor_id"`
		} `json:"profile"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.EqualValues(t, 2, saved.Version)
	require.Equal(t, supervisor, saved.Profile.SupervisorID)
}
