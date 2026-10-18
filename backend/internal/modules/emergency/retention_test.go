package emergency_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
)

func TestExpiredContactsAreNeverRevealedAndPurgePreservesHistory(t *testing.T) {
	v := vault(t)
	f := setupWithTransit(t, v)
	createBase(t, f)
	id := uuid.Must(uuid.NewV7())
	key := "helivanta-emergency-" + testutil.TenantA
	context := func(field string) []byte {
		return []byte("helivanta/emergency/case/v1|" + testutil.TenantA + "|" + f.base + "|" + id.String() + "|" + field)
	}
	caller, err := v.Encrypt(t.Context(), key, context("contact"), []byte(`{"caller_name":"Expired Synthetic Caller","callback_phone":"+910000000000","relationship":"bystander"}`))
	require.NoError(t, err)
	location, err := v.Encrypt(t.Context(), key, context("incident"), []byte(`{"location_text":"Synthetic Old Gate","patient_description":"Unknown person"}`))
	require.NoError(t, err)
	fingerprint, err := v.HMAC(t.Context(), key, context("request"))
	require.NoError(t, err)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_cases(tenant_id,base_id,id,case_number,channel,requested_service,processing_basis,policy_reference,request_fingerprint,contact_ciphertext,incident_ciphertext,created_at,updated_at,contact_retain_until) VALUES(?,?,?,?,'phone','unknown','medical_emergency','SYN-OLD',?,?,?,now()-interval '2 days',now()-interval '2 days',now()-interval '1 day')`, testutil.TenantA, f.base, id, "EC-SYN-OLD", fingerprint, caller, location).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO emergency_case_timeline(tenant_id,base_id,case_id,id,version,state,actor_subject) VALUES(?,?,?,?,1,'received','user-supervisor')`, testutil.TenantA, f.base, id, uuid.Must(uuid.NewV7())).Error
	}))
	path := f.path() + "/cases/" + id.String()
	w := testutil.Do(f.r, "GET", path, "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	out := decodeBody(t, w.Body.Bytes())
	require.Nil(t, out["contact"])
	require.Equal(t, true, out["contact_expired"])
	require.NotContains(t, w.Body.String(), "Expired Synthetic Caller")
	require.Contains(t, w.Body.String(), "Synthetic Old Gate")
	err = f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE emergency_cases SET contact_ciphertext=NULL,contact_purged_at=now() WHERE id=?`, id).Error
	})
	require.Error(t, err, "request credentials must not bypass the audited retention job")
	n, err := emergency.PurgeExpiredContacts(t.Context(), f.db, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = emergency.PurgeExpiredContacts(t.Context(), f.db, 10)
	require.NoError(t, err)
	require.Zero(t, n)
	var count int64
	var retained *string
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT contact_ciphertext FROM emergency_cases WHERE id=?`, id).Scan(&retained).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM emergency_case_timeline WHERE case_id=?`, id).Scan(&count).Error
	}))
	require.Nil(t, retained)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM emergency_audit WHERE resource_id=? AND action='case.contact_purged' AND actor_subject='system:emergency-retention'`, id).Scan(&count).Error
	}))
	require.EqualValues(t, 1, count)
	w = testutil.Do(f.r, "GET", path, "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic Old Gate")
	require.NotContains(t, w.Body.String(), "Expired Synthetic Caller")
}

func TestRetentionRejectsInvalidBatchesAndMissingSystemPool(t *testing.T) {
	for _, limit := range []int{0, -1, 1001} {
		n, err := emergency.PurgeExpiredContacts(t.Context(), &tenantdb.DB{}, limit)
		require.Error(t, err)
		require.Zero(t, n)
	}
	_, err := emergency.PurgeExpiredContacts(t.Context(), nil, 1)
	require.Error(t, err)
	_, err = emergency.PurgeExpiredContacts(t.Context(), &tenantdb.DB{}, 1)
	require.ErrorContains(t, err, "no system pool")
}
