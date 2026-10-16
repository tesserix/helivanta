package workforce

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (e taxRegimeElection) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }
func (r taxRegimeRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var errTaxRegimeInvalid = errors.New("tax-regime election is invalid")
var errTaxRegimeVersion = errors.New("tax-regime election version conflict")
var errTaxRegimeNoteRequired = errors.New("tax-regime change needs a note")

const taxRegimeColumns = "id,staff_id,tax_year,regime,intimated_on::text AS intimated_on,version,created_at,updated_at"
const taxRegimeRevisionColumns = "id,staff_id,tax_year,version,regime,intimated_on::text AS intimated_on,note,recorded_by_subject,created_at"

// validIntimation mirrors workforce_tax_regime_intimated: an intimation may precede the year but never follow its 31 March.
func validIntimation(year, day string) bool {
	start, _ := strconv.Atoi(year[:4])
	return validSalaryDate(day) && day <= strconv.Itoa(start+1)+"-03-31"
}

func taxYearParam(c *gin.Context) (string, bool) {
	year := c.Param("year")
	if !validTaxYear(year) {
		respond.NotFound(c, "tax-regime election")
		return "", false
	}
	return year, true
}

func (h *handlers) listTaxRegimes(c *gin.Context, params pagination.Params) ([]taxRegimeElection, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staff, ok := staffParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []taxRegimeElection{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx.Table("workforce_staff_tax_regimes"), params).Select(taxRegimeColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Find(&rows).Error
	})
	if err != nil {
		respondTaxRegimeError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) taxRegimeHistory(c *gin.Context, params pagination.Params) ([]taxRegimeRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staff, ok := staffParam(c)
	if !ok {
		return nil, nil
	}
	year, ok := taxYearParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []taxRegimeRevision{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Table("workforce_staff_tax_regimes").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", p.TenantID, hospital, staff, year).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx.Table("workforce_staff_tax_regime_revisions"), params).Select(taxRegimeRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", p.TenantID, hospital, staff, year).Find(&rows).Error
	})
	if err != nil {
		respondTaxRegimeError(c, err)
		return nil, nil
	}
	return rows, nil
}

// putTaxRegime records the regime the employee intimated; it is never inferred, and a change after the first needs a note.
func (h *handlers) putTaxRegime(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	year, ok := taxYearParam(c)
	if !ok {
		return
	}
	var in taxRegimeInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if !validIntimation(year, in.IntimatedOn) || !validStatutoryText(in.Note) {
		respondTaxRegimeError(c, errTaxRegimeInvalid)
		return
	}
	var result taxRegimeElection
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", salaryConfigLock); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		load := func(lock bool) error {
			q := tx.Table("workforce_staff_tax_regimes").Select(taxRegimeColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", p.TenantID, hospital, staff, year)
			if lock {
				q = q.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			return q.Take(&result).Error
		}
		version := int64(1)
		switch err := load(true); {
		case err == nil:
			var latest struct{ Note string }
			if err := tx.Table("workforce_staff_tax_regime_revisions").Select("note").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND version=?", p.TenantID, hospital, staff, year, result.Version).Take(&latest).Error; err != nil {
				return err
			}
			// Replaying the election in force, note included, returns it unchanged.
			if result.Regime == in.Regime && result.IntimatedOn == in.IntimatedOn && latest.Note == in.Note {
				return nil
			}
			if result.Version != *in.ExpectedVersion {
				return errTaxRegimeVersion
			}
			if in.Note == "" {
				return errTaxRegimeNoteRequired
			}
			version = result.Version + 1
			if err := tx.Exec(`UPDATE workforce_staff_tax_regimes SET version=?,regime=?,intimated_on=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND version=?`,
				version, in.Regime, in.IntimatedOn, p.TenantID, hospital, staff, year, result.Version).Error; err != nil {
				return err
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return errTaxRegimeVersion
		default:
			if err := tx.Exec(`INSERT INTO workforce_staff_tax_regimes(tenant_id,hospital_id,id,staff_id,tax_year,regime,intimated_on,version) VALUES(?,?,?,?,?,?,?,1)`,
				p.TenantID, hospital, uuid.New(), staff, year, in.Regime, in.IntimatedOn).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_staff_tax_regime_revisions(tenant_id,hospital_id,id,staff_id,tax_year,version,regime,intimated_on,note,recorded_by_subject) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, uuid.New(), staff, year, version, in.Regime, in.IntimatedOn, in.Note, p.Subject).Error; err != nil {
			return err
		}
		if err := recordTaxRegimeChange(h, tx, p.TenantID, hospital, p.Subject, staff, year, version); err != nil {
			return err
		}
		return load(false)
	})
	if err != nil {
		respondTaxRegimeError(c, err)
		return
	}
	respond.OK(c, result)
}

// Audit rows and events carry the staff member, tax year and version only; the regime stays behind the API.
func recordTaxRegimeChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject string, staff uuid.UUID, year string, version int64) error {
	action := "workforce.tax_regime.recorded"
	if version > 1 {
		action = "workforce.tax_regime.changed"
	}
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('tax_year',?::text,'version',?::bigint))`,
		tenant, hospital, subject, action, staff, year, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.TaxRegimeChangedData{HospitalID: hospital.String(), StaffID: staff.String(), TaxYear: year, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, TaxRegimeChanged, events.Event{Type: TaxRegimeChanged, Version: 1, TenantID: tenant, Data: data})
}

func respondTaxRegimeError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errTaxRegimeInvalid), isPG && pg.ConstraintName == "workforce_tax_regime_intimated":
		respond.Error(c, 400, "invalid_request", "Give the date the employee intimated the regime, no later than 31 March of the tax year.")
	case errors.Is(err, errTaxRegimeNoteRequired), isPG && pg.ConstraintName == "workforce_tax_regime_change_note":
		respond.Error(c, 400, "invalid_request", "Give a reason when changing a recorded regime.")
	case errors.Is(err, errTaxRegimeVersion):
		respond.Conflict(c, "The election changed since you opened it. Reload before saving.")
	default:
		respondError(c, err)
	}
}
