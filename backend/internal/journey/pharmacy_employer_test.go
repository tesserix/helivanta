package journey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type employerJourney struct {
	router *gin.Engine
	db     *tenantdb.DB
}

func newEmployerJourney(t *testing.T, owner platform.Module) *employerJourney {
	t.Helper()
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}, "reader": {workforce.PermPostingRead}},
		Modules: []platform.Module{workforce.New(), owner},
	})
	return &employerJourney{router: router, db: db}
}

func (j *employerJourney) organization(t *testing.T, tenant, ownership string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	code := strings.ToUpper(strings.ReplaceAll(id.String()[24:], "-", ""))
	require.NoError(t, j.db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), tenant, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_organizations(id,tenant_id,code,name,ownership_type) VALUES(?,?,?,?,?)`, id, tenant, "EMP"+code, "Synthetic "+ownership+" Pharmacy", ownership).Error
	}))
	return id.String()
}

func (j *employerJourney) registry(t *testing.T, id string) (rows, audits int64) {
	t.Helper()
	require.NoError(t, j.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Table("workforce_hospitals").Where("tenant_id=? AND id=?", testutil.TenantA, id).Count(&rows).Error; err != nil {
			return err
		}
		return tx.Table("workforce_audit").Where("tenant_id=? AND resource_id=? AND action='workforce_hospitals.insert'", testutil.TenantA, id).Count(&audits).Error
	}))
	return rows, audits
}

func TestStandalonePharmacyRegistersAsAnEmployerOnce(t *testing.T) {
	j := newEmployerJourney(t, pharmacy.New())
	for _, ownership := range []string{"independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			id := j.organization(t, testutil.TenantA, ownership)
			path := "/v1/workforce/pharmacies/" + id
			first := testutil.Do(j.router, "PUT", path, "admin", "{}")
			require.Equal(t, 200, first.Code, first.Body.String())
			require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
			var employer workforce.PharmacyEmployer
			require.NoError(t, json.Unmarshal(first.Body.Bytes(), &employer))
			require.Equal(t, id, employer.PharmacyID.String())
			require.Equal(t, "Synthetic "+ownership+" Pharmacy", employer.Name)
			require.Equal(t, ownership, employer.OwnershipType)
			require.WithinDuration(t, time.Now(), employer.CreatedAt, time.Minute)

			retry := testutil.Do(j.router, "PUT", path, "admin", "{}")
			require.Equal(t, 200, retry.Code, retry.Body.String())
			require.JSONEq(t, first.Body.String(), retry.Body.String(), "a retry returns the retained registration")
			read := testutil.Do(j.router, "GET", path, "admin", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
			require.JSONEq(t, first.Body.String(), read.Body.String())
			rows, audits := j.registry(t, id)
			require.EqualValues(t, 1, rows)
			require.EqualValues(t, 1, audits, "a retry must not audit a second registration")
		})
	}
}

func TestPharmacyEmployerHidesOrganizationsThatCannotEmploy(t *testing.T) {
	j := newEmployerJourney(t, pharmacy.New())
	hospital := uuid.Must(uuid.NewV7()).String()
	response := testutil.Do(j.router, "PUT", "/v1/workforce/hospitals/"+hospital, "admin", `{"name":"Synthetic Employer Hospital"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	foreign := j.organization(t, testutil.TenantB, "independent")
	for name, id := range map[string]string{
		"hospital owned":      j.organization(t, testutil.TenantA, "hospital_owned"),
		"unknown":             uuid.Must(uuid.NewV7()).String(),
		"not a version 7 id":  uuid.NewString(),
		"foreign tenant":      foreign,
		"registered hospital": hospital,
	} {
		t.Run(name, func(t *testing.T) {
			for _, method := range []string{"PUT", "GET"} {
				response := testutil.Do(j.router, method, "/v1/workforce/pharmacies/"+id, "admin", "{}")
				require.Equal(t, 404, response.Code, method+" "+response.Body.String())
				require.JSONEq(t, `{"error":"not_found","message":"pharmacy not found"}`, response.Body.String())
			}
			if id != hospital {
				rows, _ := j.registry(t, id)
				require.Zero(t, rows)
			}
		})
	}
}

func TestPharmacyEmployerRegistrationRefusesClientFacts(t *testing.T) {
	j := newEmployerJourney(t, pharmacy.New())
	id := j.organization(t, testutil.TenantA, "independent")
	response := testutil.Do(j.router, "PUT", "/v1/workforce/pharmacies/"+id, "admin", `{"name":"Synthetic Forged Name","ownership_type":"chain"}`)
	require.Equal(t, 400, response.Code, response.Body.String())
	rows, _ := j.registry(t, id)
	require.Zero(t, rows)
}

func TestPharmacyEmployerRequiresHospitalManagement(t *testing.T) {
	j := newEmployerJourney(t, pharmacy.New())
	id := j.organization(t, testutil.TenantA, "chain")
	for _, method := range []string{"PUT", "GET"} {
		response := testutil.Do(j.router, method, "/v1/workforce/pharmacies/"+id, "reader", "{}")
		require.Equal(t, 403, response.Code, method+" "+response.Body.String())
	}
	rows, _ := j.registry(t, id)
	require.Zero(t, rows)
}

type unavailableEmployerOwner struct{ *pharmacy.Module }

func (unavailableEmployerOwner) PharmacyEmployers(*tenantdb.DB) authz.PharmacyEmployerGuard {
	return unavailableEmployers{}
}

type unavailableEmployers struct{}

func (unavailableEmployers) CurrentPharmacyEmployer(context.Context, string, uuid.UUID) (authz.PharmacyEmployer, error) {
	return authz.PharmacyEmployer{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, errors.New("synthetic pharma outage"))
}

func TestPharmacyEmployerRegistrationFailsClosedWhenPharmaIsUnavailable(t *testing.T) {
	j := newEmployerJourney(t, unavailableEmployerOwner{pharmacy.New()})
	id := j.organization(t, testutil.TenantA, "independent")
	response := testutil.Do(j.router, "PUT", "/v1/workforce/pharmacies/"+id, "admin", "{}")
	require.Equal(t, 503, response.Code, response.Body.String())
	require.JSONEq(t, `{"error":"authz_unavailable","message":"authorization is temporarily unavailable"}`, response.Body.String())
	rows, _ := j.registry(t, id)
	require.Zero(t, rows)
}
