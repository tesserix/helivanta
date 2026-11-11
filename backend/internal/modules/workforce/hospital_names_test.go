package workforce_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce"                            //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract" //nolint:depguard // published contract under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (h *establishmentHarness) rename(t *testing.T, name string) {
	t.Helper()
	response := testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"`+name+`"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
}

func (h *establishmentHarness) nameVersion(t *testing.T, id string) (version int64) {
	t.Helper()
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT name_version FROM workforce_hospitals WHERE tenant_id=? AND id=?`, testutil.TenantA, id).Scan(&version).Error
	}))
	return version
}

// announced returns the decoded hospital_named payloads for one hospital, oldest first.
func (h *establishmentHarness) announced(t *testing.T, id string) []workforcecontract.HospitalNamedData {
	t.Helper()
	var raw []string
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'hospital_id'=?", workforcecontract.SubjectHospitalNamed, id).Order("created_at").Pluck("payload", &raw).Error
	}))
	out := make([]workforcecontract.HospitalNamedData, len(raw))
	for i, p := range raw {
		var evt struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(p), &evt))
		require.JSONEq(t, string(evt.Data), string(evt.Data))
		var keys map[string]any
		require.NoError(t, json.Unmarshal(evt.Data, &keys))
		require.Len(t, keys, 3, "the event carries nothing but id, name and version")
		require.NoError(t, json.Unmarshal(evt.Data, &out[i]))
	}
	return out
}

func (h *establishmentHarness) auditCount(t *testing.T, id string) (n int64) {
	t.Helper()
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, id).Scan(&n).Error
	}))
	return n
}

func TestHospitalNameVersionFollowsTheName(t *testing.T) {
	h := newEstablishmentRouter(t, workforce.New())
	h.rename(t, "Alpha Hospital")
	require.EqualValues(t, 1, h.nameVersion(t, h.hospital))
	h.rename(t, "Alpha Hospital")
	require.EqualValues(t, 1, h.nameVersion(t, h.hospital), "an unchanged name keeps its version")
	h.rename(t, "Beta Hospital")
	require.EqualValues(t, 2, h.nameVersion(t, h.hospital))
	// No writer can set the version: a direct write is overridden by the trigger.
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_hospitals SET name_version=99 WHERE tenant_id=? AND id=?`, testutil.TenantA, h.hospital).Error
	}))
	require.EqualValues(t, 2, h.nameVersion(t, h.hospital))
}

func TestAnnouncerPublishesEachHospitalNameOnce(t *testing.T) {
	h := newEstablishmentRouter(t, workforce.New())
	h.rename(t, "Alpha Hospital")
	audit := h.auditCount(t, h.hospital)
	require.Positive(t, audit, "the rename is audited, so an unchanged count is meaningful")
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	require.Equal(t, []workforcecontract.HospitalNamedData{{HospitalID: h.hospital, Name: "Alpha Hospital", NameVersion: 1}}, h.announced(t, h.hospital))
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	require.Len(t, h.announced(t, h.hospital), 1, "a second pass adds nothing")
	require.Equal(t, audit, h.auditCount(t, h.hospital), "announcing is not an audited hospital change")
}

func TestAnnouncerSendsOnlyTheNewestName(t *testing.T) {
	h := newEstablishmentRouter(t, workforce.New())
	h.rename(t, "Name A")
	h.rename(t, "Name B")
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	require.Equal(t, []workforcecontract.HospitalNamedData{{HospitalID: h.hospital, Name: "Name B", NameVersion: 2}}, h.announced(t, h.hospital))
	h.rename(t, "Name C")
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	got := h.announced(t, h.hospital)
	require.Len(t, got, 2)
	require.Equal(t, workforcecontract.HospitalNamedData{HospitalID: h.hospital, Name: "Name C", NameVersion: 3}, got[1])
}

func TestAnnouncerSkipsPharmaciesAndSuppliers(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "independent")
	supplier := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.tx(t, supplier, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,supplier_source_version) VALUES(?,?,'Synthetic Supplier','supplier',1)`, testutil.TenantA, supplier).Error
	}))
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	require.Len(t, h.announced(t, h.hospital), 1, "the hospital itself is announced")
	require.Empty(t, h.announced(t, pharmacy))
	require.Empty(t, h.announced(t, supplier))
}

func TestAnnouncerCoversSignupHospitals(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{workforce.New()}})
	resource := uuid.Must(uuid.NewV7()).String()
	in := customercontract.Workspace{TenantID: testutil.TenantA, OrganizationID: "helivanta-" + testutil.TenantA, OwnerSubject: "owner-" + testutil.TenantA, ResourceID: resource, Setup: customercontract.Setup{Name: "TEST Signup Hospital", Kind: customercontract.Hospital, CountryCode: customercontract.India, StateCode: customercontract.Odisha}}
	ctx = tenantdb.WithActor(ctx, "synthetic-customer-setup-reviewer")
	require.NoError(t, db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute),
		func(*gorm.DB) error { return nil },
		func(c context.Context) error {
			return workforce.NewCustomerWorkspaceInitializer(platform.Deps{DB: db})(c, in)
		}))
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(ctx, db))
	h := &establishmentHarness{db: db}
	require.Equal(t, []workforcecontract.HospitalNamedData{{HospitalID: resource, Name: "TEST Signup Hospital", NameVersion: 1}}, h.announced(t, resource))
}

func TestHospitalNameVersionIsForcedOnInsert(t *testing.T) {
	h := newEstablishmentRouter(t, workforce.New())
	id := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.tx(t, id, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,name_version) VALUES(?,?,'Direct Hospital',99)`, testutil.TenantA, id).Error
	}))
	require.EqualValues(t, 1, h.nameVersion(t, id), "no writer can choose the first version")
}

func TestAnnouncerPassesRacingEachOtherPublishOnce(t *testing.T) {
	h := newEstablishmentRouter(t, workforce.New())
	var ids []string
	for i := 0; i < 25; i++ {
		id := uuid.Must(uuid.NewV7()).String()
		ids = append(ids, id)
		require.NoError(t, h.tx(t, id, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name) VALUES(?,?,'Racing Hospital')`, testutil.TenantA, id).Error
		}))
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, workforce.AnnounceHospitalNamesOnce(context.Background(), h.db))
		}()
	}
	wg.Wait()
	for _, id := range ids {
		require.Len(t, h.announced(t, id), 1, "two replicas announced the same version of %s", id)
	}
}
