package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1386 A1: an outlet may sell only while a licence reviewed by a second
// person is valid on the outlet's own date.

var licencePerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.licence.manage", "pharmacy.licence.review"}

func licenceHarness(t *testing.T, now time.Time) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return now })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "owner": testutil.TenantA, "reader": testutil.TenantA, "clerk": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":  licencePerms,
			"owner":   licencePerms,
			"reader":  licencePerms,
			"clerk":   {"pharmacy.catalogue.read", "pharmacy.catalogue.write"},
			"foreign": licencePerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-owner", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	grantCatalogue(t, r, "user-clerk", "editor")
	return r, db
}

func licencePath(suffix string) string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + suffix
}

func licenceBody(from, until string) string {
	return fmt.Sprintf(`{"licence_kind":"retail","form_label":"TEST-FORM-20","licence_number":"TEST-DL-0001","issuing_authority":"TEST Drugs Controller",
		"valid_from":%q,"valid_until":%q,"pharmacist_name":"TEST Pharmacist","pharmacist_registration":"TEST-PCI-001"}`, from, until)
}

type statusBody struct {
	InForce bool   `json:"in_force"`
	Today   string `json:"today"`
	Reason  string `json:"reason"`
}

func licenceStatus(t *testing.T, r *gin.Engine) statusBody {
	t.Helper()
	w := do(r, "GET", licencePath("/licence-status"), "reader", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s statusBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func licenceInForce(t *testing.T, db *tenantdb.DB, day string) bool {
	var ok bool
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reader',true), set_config('app.pharmacy_outlet',?,true), set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT pharmacy_outlet_licence_in_force(?, ?, ?)`, testutil.TenantA, pharmaOutlet, day).Scan(&ok).Error
	}))
	return ok
}

// Review focus 1: the person who entered a licence cannot bring it into force.
func TestALicenceNeedsASecondPerson(t *testing.T) {
	r, db := licenceHarness(t, time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	require.Equal(t, "no licence has been recorded", licenceStatus(t, r).Reason)

	id := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2026-01-01", "2030-12-31"))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, http.StatusOK, do(r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2026-01-01", "2030-12-31")).Code, "replay")
	require.Equal(t, http.StatusConflict, do(r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2026-01-01", "2031-12-31")).Code)
	require.Equal(t, "a licence is awaiting review by a second person", licenceStatus(t, r).Reason)

	w = do(r, "POST", licencePath("/licences/"+id+"/review"), "editor", `{"decision":"approved","reason":"checked the original"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "four_eyes")
	require.False(t, licenceStatus(t, r).InForce)
	require.False(t, licenceInForce(t, db, "2026-11-01"), "an unreviewed licence is not in force for later slices either")

	require.Equal(t, http.StatusNotFound, do(r, "POST", licencePath("/licences/"+id+"/review"), "reader", `{"decision":"approved","reason":"x"}`).Code, "a read-only grant cannot review")
	require.Equal(t, http.StatusForbidden, do(r, "POST", licencePath("/licences/"+id+"/review"), "clerk", `{"decision":"approved","reason":"x"}`).Code)

	w = do(r, "POST", licencePath("/licences/"+id+"/review"), "owner", `{"decision":"approved","reason":"checked the original"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, licenceStatus(t, r).InForce)
	require.True(t, licenceInForce(t, db, "2026-11-01"))
	require.Equal(t, http.StatusConflict, do(r, "POST", licencePath("/licences/"+id+"/review"), "owner", `{"decision":"rejected","reason":"changed mind"}`).Code, "one decision per licence")

	// The database refuses a self-review even from a direct write.
	err := db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true), set_config('app.pharmacy_outlet',?,true), set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		other := uuid.Must(uuid.NewV7())
		if err := tx.Exec(`INSERT INTO pharmacy_outlet_licences (id,tenant_id,outlet_id,licence_kind,form_label,licence_number,issuing_authority,valid_from,valid_until,pharmacist_name,pharmacist_registration,intent_sha256,entered_by)
			VALUES (?,?,?,'retail','F','N','A','2026-01-01','2030-01-01','P','R',repeat('a',64),'user-editor')`, other, testutil.TenantA, pharmaOutlet).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_outlet_licence_reviews (tenant_id,outlet_id,licence_id,decision,reason,reviewed_by) VALUES (?,?,?,'approved','x','user-editor')`,
			testutil.TenantA, pharmaOutlet, other).Error
	})
	require.ErrorContains(t, err, "reviewed by someone other than who entered it")
}

// Review focus 2: validity is judged on the outlet's date. At 19:00 UTC on
// 31 Dec it is already 1 Jan in India.
func TestALicenceIsJudgedOnTheOutletsDate(t *testing.T) {
	r, db := licenceHarness(t, time.Date(2026, 12, 31, 19, 0, 0, 0, time.UTC))
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2022-01-01", "2026-12-31")).Code)
	require.Equal(t, http.StatusOK, do(r, "POST", licencePath("/licences/"+id+"/review"), "owner", `{"decision":"approved","reason":"ok"}`).Code)
	s := licenceStatus(t, r)
	require.Equal(t, "2027-01-01", s.Today)
	require.False(t, s.InForce, "expired yesterday in India")
	require.Equal(t, "no approved licence is valid today", s.Reason)

	renewed := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", licencePath("/licences/"+renewed), "editor", licenceBody("2027-01-01", "2031-12-31")).Code)
	require.Equal(t, http.StatusOK, do(r, "POST", licencePath("/licences/"+renewed+"/review"), "owner", `{"decision":"rejected","reason":"number does not match the certificate"}`).Code)
	require.False(t, licenceStatus(t, r).InForce, "a rejected renewal is not a licence")
	require.False(t, licenceInForce(t, db, "2027-01-01"))
	require.True(t, licenceInForce(t, db, "2026-12-31"), "the old licence still answered for its own last day")
}

func TestLicenceInputAndScope(t *testing.T) {
	r, _ := licenceHarness(t, time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	for _, body := range []string{
		licenceBody("2026-01-01", "2025-12-31"),
		licenceBody("2026-13-01", "2027-01-01"),
		`{"licence_kind":"chain","form_label":"F","licence_number":"N","issuing_authority":"A","valid_from":"2026-01-01","valid_until":"2027-01-01","pharmacist_name":"P","pharmacist_registration":"R"}`,
		`{"licence_kind":"retail","form_label":" ","licence_number":"N","issuing_authority":"A","valid_from":"2026-01-01","valid_until":"2027-01-01","pharmacist_name":"P","pharmacist_registration":"R"}`,
	} {
		require.Equal(t, http.StatusBadRequest, do(r, "PUT", licencePath("/licences/"+uuid.Must(uuid.NewV7()).String()), "editor", body).Code, body)
	}
	require.Equal(t, http.StatusNotFound, do(r, "GET", licencePath("/licence-status"), "foreign", "").Code)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", licencePath("/licences/"+id), "editor", licenceBody("2026-01-01", "2030-12-31")).Code)
	w := do(r, "GET", licencePath("/licences"), "reader", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Data []struct {
			ID       string  `json:"id"`
			Decision *string `json:"decision"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Nil(t, list.Data[0].Decision)
}
