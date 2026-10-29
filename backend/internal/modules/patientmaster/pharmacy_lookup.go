package patientmaster

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// pharmacyLookupGrant lets one named pharmacist look up this hospital's
// patients for one pharmacy, in the link's current round (#1416b).
type pharmacyLookupGrant struct {
	Subject    string    `json:"subject"`
	PharmacyID uuid.UUID `json:"pharmacy_id"`
	GrantedBy  string    `json:"granted_by"`
	CreatedAt  time.Time `json:"created_at"`
	ID         uuid.UUID `json:"-"`
}

func (pharmacyLookupGrant) TableName() string { return "patient_pharmacy_lookup_access" }

// PageKey pages grants by id; created_at orders them newest first.
func (g pharmacyLookupGrant) PageKey() (time.Time, uuid.UUID) { return g.CreatedAt, g.ID }

const lookupGrantColumns = "id,subject,pharmacy_id,granted_by,created_at"

var errNoActiveLink = errors.New("the pharmacy has no active link with this hospital")

// activeLinkRound is the link's current round, or errNoActiveLink.
func activeLinkRound(tx *gorm.DB, tenant string, hospital, pharmacy uuid.UUID) (uuid.UUID, error) {
	var link []struct{ RequestID uuid.UUID }
	if err := tx.Raw(`SELECT request_id FROM patient_pharmacy_links WHERE tenant_id=? AND hospital_id=? AND pharmacy_id=? FOR SHARE`, tenant, hospital, pharmacy).Scan(&link).Error; err != nil {
		return uuid.Nil, err
	}
	if len(link) != 1 {
		return uuid.Nil, errNoActiveLink
	}
	return link[0].RequestID, nil
}

func lookupGrantTarget(c *gin.Context) (hospital, pharmacy uuid.UUID, subject string, ok bool) {
	c.Header("Cache-Control", "no-store")
	h, err1 := uuid.Parse(c.Param("hospital"))
	ph, err2 := uuid.Parse(c.Param("pharmacy"))
	subject = c.Param("subject")
	if err1 != nil || err2 != nil || h == uuid.Nil || ph == uuid.Nil || strings.TrimSpace(subject) != subject || subject == "" || len(subject) > 200 {
		respond.NotFound(c, "pharmacy lookup access")
		return uuid.Nil, uuid.Nil, "", false
	}
	return h, ph, subject, true
}

func (h *patientHandlers) putPharmacyLookupAccess(c *gin.Context) {
	h.changePharmacyLookupAccess(c, false)
}
func (h *patientHandlers) deletePharmacyLookupAccess(c *gin.Context) {
	h.changePharmacyLookupAccess(c, true)
}

func (h *patientHandlers) changePharmacyLookupAccess(c *gin.Context, revoke bool) {
	// The tenant precondition answers before any input is judged.
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	hospital, pharmacy, subject, ok := lookupGrantTarget(c)
	if !ok {
		return
	}
	if !revoke {
		var none struct{}
		if !decodePatientInput(c, &none) {
			return
		}
	}
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		action := "grant"
		var changed *gorm.DB
		if revoke {
			action = "revoke"
			changed = tx.Exec(`DELETE FROM patient_pharmacy_lookup_access WHERE tenant_id=? AND hospital_id=? AND pharmacy_id=? AND subject=?`, p.TenantID, hospital, pharmacy, subject)
		} else {
			round, err := activeLinkRound(tx, p.TenantID, hospital, pharmacy)
			if err != nil {
				return err
			}
			changed = tx.Exec(`INSERT INTO patient_pharmacy_lookup_access (tenant_id,hospital_id,pharmacy_id,request_id,subject,granted_by) VALUES (?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
				p.TenantID, hospital, pharmacy, round, subject, p.Subject)
		}
		if changed.Error != nil {
			return changed.Error
		}
		// A repeat changes nothing and is answered the same, but records nothing.
		if changed.RowsAffected == 0 {
			return nil
		}
		return tx.Exec(`INSERT INTO patient_access_audit (tenant_id,hospital_id,actor_subject,action,target_subject,access_level,pharmacy_id) VALUES (?,?,?,?,?,'pharmacy_lookup',?)`,
			p.TenantID, hospital, p.Subject, action, subject, pharmacy).Error
	})
	if errors.Is(err, errNoActiveLink) {
		respond.Error(c, http.StatusConflict, "no_active_link", "This pharmacy's link with the hospital is not verified on both sides yet.")
		return
	}
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "pharmacy_id": pharmacy, "subject": subject, "granted": !revoke})
}

// listPharmacyLookupAccess lists the grants in force: those of the link's
// current round. Grants of an ended round are inert and not shown.
func (h *patientHandlers) listPharmacyLookupAccess(c *gin.Context, params pagination.Params) ([]pharmacyLookupGrant, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	hospital, err1 := uuid.Parse(c.Param("hospital"))
	pharmacy, err2 := uuid.Parse(c.Query("pharmacy_id"))
	if err1 != nil || err2 != nil || hospital == uuid.Nil || pharmacy == uuid.Nil {
		respond.NotFound(c, "pharmacy lookup access")
		return nil, nil
	}
	rows := []pharmacyLookupGrant{}
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		round, err := activeLinkRound(tx, p.TenantID, hospital, pharmacy)
		if errors.Is(err, errNoActiveLink) {
			return nil
		}
		if err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(lookupGrantColumns).
			Where("tenant_id=? AND hospital_id=? AND pharmacy_id=? AND request_id=?", p.TenantID, hospital, pharmacy, round).Find(&rows).Error
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}

// pharmacyLookupResult is the least a pharmacist needs to confirm the
// person in front of them: no MRN echo, date of birth, contact or identifier.
type pharmacyLookupResult struct {
	PatientID    uuid.UUID `json:"patient_id"`
	Name         string    `json:"name"`
	AgeYears     int       `json:"age_years"`
	AgeEstimated bool      `json:"age_estimated"`
	Sex          string    `json:"sex"`
}

// pharmacyLookup finds one patient of the hospital by exact MRN for a named
// pharmacist of a pharmacy whose link is active on both sides. No grant, no
// link and no match all answer the same 404, and every lookup is audited.
func (h *patientHandlers) pharmacyLookup(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	hospital, err1 := uuid.Parse(c.Query("hospital_id"))
	pharmacy, err2 := uuid.Parse(c.Query("pharmacy_id"))
	mrn := strings.ToUpper(strings.TrimSpace(c.Query("mrn")))
	// Or the patient id, for a stay resolved from a bed (#1387): exactly one.
	var patientID *uuid.UUID
	if raw := c.Query("patient_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "patient_id must be a patient id")
			return
		}
		patientID = &id
	}
	// Age uses the same UTC calendar registration checks dates of birth
	// against: patientmaster holds no hospital time zone. It is an aid to
	// recognising the person, never a dosing or eligibility input.
	if err1 != nil || err2 != nil || hospital == uuid.Nil || pharmacy == uuid.Nil || (mrn == "") == (patientID == nil) || len(mrn) > 64 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "hospital_id, pharmacy_id and exactly one of mrn or patient_id are required")
		return
	}
	patientSetting := ""
	if patientID != nil {
		patientSetting = patientID.String()
	}
	var found []pharmacyLookupResult
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_lookup_subject',?,true),set_config('app.pharmacy_lookup_hospital',?,true),set_config('app.pharmacy_lookup_pharmacy',?,true),set_config('app.pharmacy_lookup_mrn',?,true),set_config('app.pharmacy_lookup_patient',?,true)`,
			p.Subject, hospital.String(), pharmacy.String(), mrn, patientSetting).Error; err != nil {
			return err
		}
		// Lock the grant and the link until commit, so a revocation or a
		// withdrawal cannot race an answer.
		var held int64
		if err := tx.Raw(`SELECT count(*) FROM (SELECT 1 FROM patient_pharmacy_links l JOIN patient_pharmacy_lookup_access a
 ON a.tenant_id=l.tenant_id AND a.hospital_id=l.hospital_id AND a.pharmacy_id=l.pharmacy_id AND a.request_id=l.request_id
 WHERE l.tenant_id=? AND l.hospital_id=? AND l.pharmacy_id=? AND a.subject=? FOR SHARE OF l, a) held`, p.TenantID, hospital, pharmacy, p.Subject).Scan(&held).Error; err != nil {
			return err
		}
		if held == 1 {
			if err := tx.Raw(`SELECT id AS patient_id, btrim(given_name||' '||family_name) AS name,
 date_part('year', age(current_date, dob))::int AS age_years, dob_estimated AS age_estimated, sex
 FROM patients WHERE tenant_id=? AND hospital_id=? AND (mrn=NULLIF(?,'') OR id=NULLIF(?,'')::uuid)`, p.TenantID, hospital, mrn, patientSetting).Scan(&found).Error; err != nil {
				return err
			}
		}
		ids := "[]"
		if len(found) == 1 {
			ids = `["` + found[0].PatientID.String() + `"]`
		}
		return tx.Exec(`INSERT INTO patient_access_audit (tenant_id,hospital_id,actor_subject,action,resource_ids,pharmacy_id) VALUES (?,?,?,'pharmacy_lookup',?::jsonb,?)`,
			p.TenantID, hospital, p.Subject, ids, pharmacy).Error
	})
	if err != nil {
		// Database errors may embed demographics; only the operation is logged.
		respond.InternalErr(c, errors.New("pharmacy patient lookup failed"), "could not look up the patient")
		return
	}
	if len(found) != 1 {
		respond.NotFound(c, "patient")
		return
	}
	respond.OK(c, found[0])
}
