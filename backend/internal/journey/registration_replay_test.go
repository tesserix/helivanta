package journey

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/patientmaster"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

// #1377: a patient registered before MediCore listened cannot open an
// encounter until patientmaster replays the registration; afterwards they can.
func TestReplayedRegistrationMakesAnExistingPatientOpenable(t *testing.T) {
	const hospital = "0199e000-0000-7000-8000-00000000a001"
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"doc": testutil.TenantA, "admin": testutil.TenantA, "reg": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"doc":   {medicore.PermVisitCreate, medicore.PermVisitRead},
			"reg":   {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
			"admin": {patientmaster.PermRegistrationReplay, medicore.PermAccessManage},
		},
		Modules: []platform.Module{patientmaster.New(), medicore.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		for _, subject := range []string{"user-admin", "user-reg"} {
			if err := tx.Exec(`INSERT INTO patient_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, ?, 'registrar')`, testutil.TenantA, hospital, subject).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	w := do(r, "POST", "/v1/patients?hospital_id="+hospital, "reg", fmt.Sprintf(`{"request_id":%q,"given_name":"TEST","family_name":"Replay","dob":"1990-01-01","mobile":"9876500000","notice_version":"v1","consented_by":"patient"}`, uuid.NewString()))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var registered struct {
		Patient struct {
			ID uuid.UUID `json:"id"`
		} `json:"patient"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &registered))
	patient := registered.Patient.ID
	// A patient registered before the projection existed: present in
	// patientmaster, absent from MediCore. Simulated by waiting for the live
	// event to land, then removing MediCore's copy.
	projected := func() (n int64) {
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM medicore_patients WHERE patient_id = ?`, patient).Scan(&n).Error
		}))
		return n
	}
	require.Eventually(t, func() bool { return projected() == 1 }, 30*time.Second, 250*time.Millisecond)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM medicore_patients WHERE patient_id = ?`, patient).Error
	}))
	require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/medicore/hospitals/"+hospital+"/access/user-doc", "admin", "").Code)
	open := func() int {
		body := fmt.Sprintf(`{"patient_id":%q,"consult_mode":"in_person","visit_type":"first"}`, patient)
		return do(r, "PUT", "/v1/medicore/hospitals/"+hospital+"/encounters/"+uuid.Must(uuid.NewV7()).String(), "doc", body).Code
	}
	require.Equal(t, http.StatusNotFound, open(), "not yet known to MediCore")

	w = do(r, "POST", "/v1/patients/hospitals/"+hospital+"/registration-replays", "admin", `{}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var replayed struct {
		Data []struct {
			PatientID string `json:"patient_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &replayed))
	require.Len(t, replayed.Data, 1)
	require.Eventually(t, func() bool { return open() == http.StatusCreated }, 30*time.Second, 250*time.Millisecond)
}
