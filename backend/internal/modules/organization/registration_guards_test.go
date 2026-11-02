package organization_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestRegistrationRequiresExactWorkspaceAndBoundedIntent(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA}, Perms: map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read"}, "reader": {"organization.structure.read"}}, Modules: []platform.Module{organization.New()}})
	hospital := uuid.NewString()
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + id
	for _, body := range []string{`{}`, `null`, `[]`, `{"reason":""}`, `{"reason":"   "}`, `{"reason":"ok","extra":true}`, `{"reason":"ok"}{}`, `{"reason":"\u0000"}`, fmt.Sprintf(`{"reason":%q}`, strings.Repeat("x", 501))} {
		w := organizationCall(r, "PUT", path, "manager", testutil.TenantA, body)
		require.Equal(t, 400, w.Code, body+": "+w.Body.String())
	}
	req := httptest.NewRequest("PUT", path, strings.NewReader(`{"reason":"Synthetic missing assertions"}`))
	req.Header.Set("Authorization", "Bearer manager")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 409, organizationCall(r, "PUT", path, "manager", testutil.TenantB, `{"reason":"Synthetic switched tenant"}`).Code)
	require.Equal(t, 403, organizationCall(r, "PUT", path, "outsider", testutil.TenantA, `{"reason":"Synthetic missing permission"}`).Code)
	require.Equal(t, 404, organizationCall(r, "PUT", strings.Replace(path, id, uuid.NewString(), 1), "manager", testutil.TenantA, `{"reason":"Synthetic old command id"}`).Code)
	require.Equal(t, 404, organizationCall(r, "PUT", strings.Replace(path, hospital, uuid.Nil.String(), 1), "manager", testutil.TenantA, `{"reason":"Synthetic empty hospital"}`).Code)
	require.Equal(t, 403, organizationCall(r, "GET", "/v1/organization/hospitals", "outsider", testutil.TenantA, "").Code)
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("organization_hospital_requests").Count(&count).Error }))
	require.Zero(t, count)
	w = organizationCall(r, "PUT", path, "manager", testutil.TenantA, `{"reason":"  Synthetic   normalized  intent  "}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	retry := organizationCall(r, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic normalized intent"}`)
	require.JSONEq(t, w.Body.String(), retry.Body.String())
	other := "/v1/organization/hospitals/" + uuid.NewString() + "/registrations/" + id
	require.Equal(t, 409, organizationCall(r, "PUT", other, "manager", testutil.TenantA, `{"reason":"Synthetic normalized intent"}`).Code)
	w = organizationCall(r, "GET", "/v1/organization/hospitals?limit=1", "reader", testutil.TenantA, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), hospital)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.organization.hospital_reference_requested.v1").Count(&count).Error
	}))
	require.EqualValues(t, 1, count)
}

func TestOutboxFailureRollsBackRegistrationAndAudit(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"manager": testutil.TenantA}, Perms: map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read"}}, Modules: []platform.Module{organizationOutboxFailureModule{organization.New()}}})
	hospital := uuid.NewString()
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + id
	w := organizationCall(r, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic atomicity check"}`)
	require.Equal(t, 500, w.Code, w.Body.String())
	for _, table := range []string{"organization_hospital_requests", "organization_hospital_registrations", "outbox_events"} {
		var count int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table(table).Count(&count).Error }))
		require.Zero(t, count, table)
	}
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("action='hospital_reference_requested'").Count(&count).Error
	}))
	require.Zero(t, count)
}

type organizationOutboxFailureModule struct{ *organization.Module }

func (m organizationOutboxFailureModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9000_organization_test_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT organization_test_outbox_fault CHECK(subject<>'helivanta.in.organization.hospital_reference_requested.v1') NOT VALID;`})
}
