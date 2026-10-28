package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1386 B-2a: the prescription record and its scanned pages.

var prescriptionPerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.prescription.record", "pharmacy.prescription.read"}

type prescriptionWorld struct {
	r       *gin.Engine
	db      *tenantdb.DB
	bus     *events.Bus
	files   *attachments.GCS
	product string
}

// newPrescriptionWorld is an outlet on 1 Nov 2026 (IST). files may be nil,
// which is a deployment without private storage.
func newPrescriptionWorld(t *testing.T, files *attachments.GCS) prescriptionWorld {
	t.Helper()
	return newPrescriptionWorldWith(t, files, files != nil)
}

// newPrescriptionWorldWith sets the rollout flag apart from storage.
func newPrescriptionWorldWith(t *testing.T, files *attachments.GCS, evidence bool) prescriptionWorld {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "viewer": testutil.TenantA, "owner": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     prescriptionPerms,
			"pharmacist": prescriptionPerms,
			"viewer":     prescriptionPerms,
			"owner":      {"pharmacy.catalogue.read", "pharmacy.sale.read", "pharmacy.stock.read"},
			"foreign":    prescriptionPerms,
		}, Modules: []platform.Module{pharmacy.New()}, Documents: files, PharmacyEvidenceEnabled: evidence,
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-pharmacist", "editor")
	grantCatalogue(t, r, "user-owner", "editor")
	grantCatalogue(t, r, "user-viewer", "reader")
	product := uuid.Must(uuid.NewV7()).String()
	res := do(r, "PUT", productPath(product), "editor", productBody(0, "TEST Amoxicillin 500"))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	return prescriptionWorld{r: r, db: db, bus: bus, files: files, product: product}
}

func prescriptionPath(suffix string) string { return licencePath("/prescriptions" + suffix) }

// rxLines is one prescribed line: units in total, over that many dispensings.
func rxLines(product string, units, dispensings int) string {
	return fmt.Sprintf(`,"lines":[{"prescribed_as":"TEST Amoxicillin 500 mg","product_id":%q,"quantity_units":%d,"dispensings_allowed":%d}]`, product, units, dispensings)
}

func (w prescriptionWorld) body(date, address string) string {
	addr := ""
	if address != "" {
		addr = fmt.Sprintf(`,"patient_address":%q`, address)
	}
	return fmt.Sprintf(`{"prescriber_name":"TEST Dr Asha Rao","prescriber_registration":"MMC/2011/04567","prescriber_council":"Maharashtra Medical Council","prescribed_on":%q,"patient_name":"TEST Ravi Kumar"%s%s}`, date, addr, rxLines(w.product, 20, 1))
}

func (w prescriptionWorld) record(t *testing.T) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", w.body("2026-10-30", "TEST 4 Station Road, Pune"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	return id
}

func TestAPrescriptionIsRecordedOnceAndReadOnlyByThePharmacist(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	id := uuid.Must(uuid.NewV7()).String()
	body := w.body("2026-10-30", "")

	created := do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var rx struct {
		ID             string  `json:"id"`
		PrescribedOn   string  `json:"prescribed_on"`
		RecordedBy     string  `json:"recorded_by"`
		PatientAddress *string `json:"patient_address"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &rx))
	require.Equal(t, id, rx.ID)
	require.Equal(t, "2026-10-30", rx.PrescribedOn)
	require.Equal(t, "user-pharmacist", rx.RecordedBy)
	require.Nil(t, rx.PatientAddress)

	require.Equal(t, http.StatusOK, do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", body).Code, "the same intent replays")
	conflict := do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", w.body("2026-10-29", ""))
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), "already recorded with different details")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", prescriptionPath("/"+id), "editor", body).Code, "another recorder is another intent")

	// Today is 1 Nov in the outlet's timezone, though it is still 1 Nov in UTC too:
	// 2 Nov is the future wherever the server runs.
	future := do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", w.body("2026-11-02", ""))
	require.Equal(t, http.StatusBadRequest, future.Code, future.Body.String())
	for _, bad := range []string{
		`{"prescriber_name":" TEST Dr","prescriber_registration":"R1","prescriber_council":"C","prescribed_on":"2026-10-30","patient_name":"P"` + rxLines(w.product, 20, 1) + `}`,
		`{"prescriber_name":"TEST Dr","prescriber_registration":"R1;drop","prescriber_council":"C","prescribed_on":"2026-10-30","patient_name":"P"` + rxLines(w.product, 20, 1) + `}`,
		`{"prescriber_name":"TEST Dr","prescriber_registration":"R1","prescriber_council":"C","prescribed_on":"30/10/2026","patient_name":"P"` + rxLines(w.product, 20, 1) + `}`,
		`{"prescriber_name":"TEST Dr","prescriber_registration":"R1","prescriber_council":"C","prescribed_on":"2026-10-30"` + rxLines(w.product, 20, 1) + `}`,
		`{"prescriber_name":"TEST Dr\u0007","prescriber_registration":"R1","prescriber_council":"C","prescribed_on":"2026-10-30","patient_name":"P"` + rxLines(w.product, 20, 1) + `}`,
	} {
		res := do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", bad)
		require.Equal(t, http.StatusBadRequest, res.Code, bad)
	}

	got := do(w.r, "GET", prescriptionPath("/"+id), "viewer", "")
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	list := do(w.r, "GET", prescriptionPath("?limit=10"), "viewer", "")
	require.Equal(t, http.StatusOK, list.Code)
	require.Contains(t, list.Body.String(), id)

	require.Equal(t, http.StatusNotFound, do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "viewer", body).Code, "a reader grant cannot record")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", prescriptionPath("/"+id), "owner", "").Code, "the owner holds no prescription permission")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", prescriptionPath(""), "owner", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", prescriptionPath("/open-lines?product_id="+w.product), "owner", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", prescriptionPath("/"+id), "foreign", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "viewer", "").Code)
}

func TestPrescriptionsAreInsertOnlyAndOutletScoped(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	id := w.record(t)
	session := func(tx *gorm.DB, subject, outlet string) {
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, subject, outlet, pharmaOrganization).Error)
	}
	err := w.db.WithTenant(tenantdb.WithActor(t.Context(), "test"), testutil.TenantA, func(tx *gorm.DB) error {
		session(tx, "user-pharmacist", pharmaOutlet)
		return tx.Exec(`UPDATE pharmacy_prescriptions SET patient_name='TEST Someone Else' WHERE id=?`, id).Error
	})
	require.Error(t, err, "a recorded prescription cannot be rewritten")
	err = w.db.WithTenant(tenantdb.WithActor(t.Context(), "test"), testutil.TenantA, func(tx *gorm.DB) error {
		session(tx, "user-pharmacist", pharmaOutlet)
		return tx.Exec(`DELETE FROM pharmacy_prescriptions WHERE id=?`, id).Error
	})
	require.Error(t, err, "a recorded prescription cannot be deleted")
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		session(tx, "user-pharmacist", uuid.Must(uuid.NewV7()).String())
		require.NoError(t, tx.Raw(`SELECT count(*) FROM pharmacy_prescriptions`).Scan(&n).Error)
		require.Zero(t, n, "another outlet's session sees no prescription")
		session(tx, "user-nobody", pharmaOutlet)
		require.NoError(t, tx.Raw(`SELECT count(*) FROM pharmacy_prescriptions`).Scan(&n).Error)
		require.Zero(t, n, "a subject with no grant sees no prescription")
		return nil
	}))
}

// #1386 B-2b decision 1: a prescription records what was prescribed.
func TestAPrescriptionRecordsWhatWasPrescribed(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	id := uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(w.body("2026-10-30", "TEST 4 Station Road, Pune"), `"patient_name"`, `"prescriber_address":"TEST 12 Clinic Lane, Pune","patient_name"`, 1)
	body = strings.Replace(body, rxLines(w.product, 20, 1), rxLines(w.product, 30, 3), 1)
	res := do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var rx struct {
		PrescriberAddress *string `json:"prescriber_address"`
		Lines             []struct {
			LineNumber           int    `json:"line_number"`
			PrescribedAs         string `json:"prescribed_as"`
			ProductID            string `json:"product_id"`
			QuantityUnits        int64  `json:"quantity_units"`
			DispensingsAllowed   int    `json:"dispensings_allowed"`
			UnitsRemaining       int64  `json:"units_remaining"`
			DispensingsRemaining int    `json:"dispensings_remaining"`
		} `json:"lines"`
	}
	got := do(w.r, "GET", prescriptionPath("/"+id), "viewer", "")
	require.Equal(t, http.StatusOK, got.Code)
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &rx))
	require.NotNil(t, rx.PrescriberAddress)
	require.Equal(t, "TEST 12 Clinic Lane, Pune", *rx.PrescriberAddress)
	require.Len(t, rx.Lines, 1)
	l := rx.Lines[0]
	require.Equal(t, 1, l.LineNumber)
	require.Equal(t, "TEST Amoxicillin 500 mg", l.PrescribedAs)
	require.Equal(t, w.product, l.ProductID)
	require.EqualValues(t, 30, l.QuantityUnits)
	require.Equal(t, 3, l.DispensingsAllowed)
	require.EqualValues(t, 30, l.UnitsRemaining)
	require.Equal(t, 3, l.DispensingsRemaining)

	require.Equal(t, http.StatusOK, do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", body).Code, "the same lines replay")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", prescriptionPath("/"+id), "pharmacist", strings.Replace(body, `"quantity_units":30`, `"quantity_units":31`, 1)).Code)

	// A prescription without lines is refused, as is one beyond 20 lines.
	noLines := strings.Replace(w.body("2026-10-30", ""), rxLines(w.product, 20, 1), "", 1)
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", noLines).Code)
	one := strings.TrimPrefix(strings.TrimSuffix(rxLines(w.product, 1, 1), "]"), `,"lines":[`)
	many := strings.Repeat(one+",", 20) + one
	tooMany := strings.Replace(w.body("2026-10-30", ""), rxLines(w.product, 20, 1), `,"lines":[`+many+`]`, 1)
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", tooMany).Code)
	for _, bad := range []string{`"dispensings_allowed":13`, `"dispensings_allowed":0`, `"quantity_units":0`} {
		key := bad[:strings.Index(bad, ":")+1]
		body := w.body("2026-10-30", "")
		start := strings.Index(body, key)
		end := start + strings.IndexAny(body[start:], ",}")
		res := do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", body[:start]+bad+body[end:])
		require.Equal(t, http.StatusBadRequest, res.Code, bad)
	}
	// Another outlet's product, or one that does not exist, is not this outlet's.
	missing := strings.Replace(w.body("2026-10-30", ""), w.product, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, http.StatusNotFound, do(w.r, "PUT", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", missing).Code)
}
