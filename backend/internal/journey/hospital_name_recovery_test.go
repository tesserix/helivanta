package journey

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// A hospital name whose delivery dead-letters still reaches pharmacy (#1653):
// the announcer re-sends it until pharmacy's confirmation comes back.
type nameRecovery struct {
	router   *gin.Engine
	db       *tenantdb.DB
	hospital string
	names    events.Consumer
	receipts events.Consumer
}

func consumer(t *testing.T, m platform.Module, name string) events.Consumer {
	t.Helper()
	for _, c := range m.Consumers(platform.Deps{}) {
		if c.Name == name {
			return c
		}
	}
	require.FailNow(t, name+" consumer is not registered")
	return events.Consumer{}
}

func newNameRecovery(t *testing.T) *nameRecovery {
	t.Helper()
	wf, ph := workforce.New(), pharmacy.New()
	router, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{wf, ph},
	})
	return &nameRecovery{router: router, db: db, hospital: uuid.Must(uuid.NewV7()).String(),
		names: consumer(t, ph, "pharmacy-hospital-names"), receipts: consumer(t, wf, "workforce-hospital-name-receipt")}
}

func (r *nameRecovery) rename(t *testing.T, name string) {
	t.Helper()
	response := testutil.Do(r.router, "PUT", "/v1/workforce/hospitals/"+r.hospital, "admin", `{"name":"`+name+`"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
}

func (r *nameRecovery) pass(t *testing.T) {
	t.Helper()
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), r.db))
}

// resend makes the marker due and runs a pass, which must add exactly one
// announcement: the one handed to pharmacy next, never the lost original.
func (r *nameRecovery) resend(t *testing.T) {
	t.Helper()
	before := len(r.outbox(t, workforcecontract.SubjectHospitalNamed))
	r.makeDue(t)
	r.pass(t)
	require.Len(t, r.outbox(t, workforcecontract.SubjectHospitalNamed), before+1, "the due name was re-sent")
}

func (r *nameRecovery) makeDue(t *testing.T) {
	t.Helper()
	require.NoError(t, r.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_hospital_name_announcements SET next_attempt_at=now()-interval '1 second' WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, r.hospital).Error
	}))
}

// outbox returns this hospital's payloads on subject, oldest first.
func (r *nameRecovery) outbox(t *testing.T, subject string) []events.Event {
	t.Helper()
	var raw []string
	require.NoError(t, r.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'hospital_id'=?", subject, r.hospital).Order("created_at").Pluck("payload", &raw).Error
	}))
	out := make([]events.Event, len(raw))
	for i, p := range raw {
		require.NoError(t, json.Unmarshal([]byte(p), &out[i]))
	}
	return out
}

func (r *nameRecovery) newest(t *testing.T, subject string) events.Event {
	t.Helper()
	all := r.outbox(t, subject)
	require.NotEmpty(t, all, subject)
	return all[len(all)-1]
}

func (r *nameRecovery) handle(t *testing.T, c events.Consumer, evt events.Event) {
	t.Helper()
	require.NoError(t, r.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return c.Handle(t.Context(), tx, evt) }))
}

// toPharmacy delivers the newest announcement to pharmacy.
func (r *nameRecovery) toPharmacy(t *testing.T) {
	t.Helper()
	r.handle(t, r.names, r.newest(t, workforcecontract.SubjectHospitalNamed))
}

// toWorkforce delivers pharmacy's newest confirmation back to workforce.
func (r *nameRecovery) toWorkforce(t *testing.T) {
	t.Helper()
	r.handle(t, r.receipts, r.newest(t, pharmacycontract.SubjectHospitalNameReceived))
}

func (r *nameRecovery) pharmacyName(t *testing.T) (name string) {
	t.Helper()
	require.NoError(t, r.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT name FROM pharmacy_hospital_names WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, r.hospital).Scan(&name).Error
	}))
	return name
}

func (r *nameRecovery) next(t *testing.T) *time.Time {
	t.Helper()
	var row struct{ NextAttemptAt *time.Time }
	require.NoError(t, r.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT next_attempt_at FROM workforce_hospital_name_announcements WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, r.hospital).Scan(&row).Error
	}))
	return row.NextAttemptAt
}

// lost leaves pharmacy without the current name: the first announcement, or
// a rename after an earlier name arrived, is announced and dead-letters.
func lost(t *testing.T, rename bool) *nameRecovery {
	t.Helper()
	r := newNameRecovery(t)
	r.rename(t, "Alpha Hospital")
	r.pass(t)
	want := ""
	if rename {
		r.toPharmacy(t)
		r.toWorkforce(t)
		r.rename(t, "Beta Hospital")
		r.pass(t)
		want = "Alpha Hospital"
	}
	require.Equal(t, want, r.pharmacyName(t), "the announcement was not delivered")
	return r
}

func currentName(rename bool) string {
	if rename {
		return "Beta Hospital"
	}
	return "Alpha Hospital"
}

func TestDeadLetteredHospitalNameRecovers(t *testing.T) {
	for name, rename := range map[string]bool{"first announcement": false, "rename": true} {
		t.Run(name, func(t *testing.T) {
			r := lost(t, rename)
			r.resend(t)
			r.toPharmacy(t)
			r.toWorkforce(t)
			require.Equal(t, currentName(rename), r.pharmacyName(t))
			require.Nil(t, r.next(t), "pharmacy's confirmation stops the re-sending")
			sent := len(r.outbox(t, workforcecontract.SubjectHospitalNamed))
			r.pass(t)
			require.Len(t, r.outbox(t, workforcecontract.SubjectHospitalNamed), sent, "a confirmed name is not re-sent")
		})
	}
}

func TestDeadLetteredHospitalNameRecoversAfterALostConfirmation(t *testing.T) {
	for name, rename := range map[string]bool{"first announcement": false, "rename": true} {
		t.Run(name, func(t *testing.T) {
			r := lost(t, rename)
			r.resend(t)
			r.toPharmacy(t) // its confirmation is dropped
			require.Equal(t, currentName(rename), r.pharmacyName(t))
			require.NotNil(t, r.next(t))
			r.resend(t)
			r.toPharmacy(t)
			r.toWorkforce(t)
			require.Equal(t, currentName(rename), r.pharmacyName(t))
			require.Nil(t, r.next(t))
			sent := len(r.outbox(t, workforcecontract.SubjectHospitalNamed))
			r.pass(t)
			require.Len(t, r.outbox(t, workforcecontract.SubjectHospitalNamed), sent)
		})
	}
}
