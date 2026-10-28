package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// A prescription is the outlet's record of what a prescriber wrote (#1386
// B-2a). It is insert-only: a posted sale may cite it, so a mistake is a new
// record and the old one stays.

type prescriptionInput struct {
	PrescriberName         string  `json:"prescriber_name" binding:"required"`
	PrescriberRegistration string  `json:"prescriber_registration" binding:"required"`
	PrescriberCouncil      string  `json:"prescriber_council" binding:"required"`
	PrescribedOn           string  `json:"prescribed_on" binding:"required"`
	PatientName            string  `json:"patient_name" binding:"required"`
	PatientAddress         *string `json:"patient_address"`
	// From B-2b: who wrote it, where, and what was prescribed (Rule 65(3), 65(10)).
	PrescriberAddress *string                 `json:"prescriber_address"`
	Lines             []prescriptionLineInput `json:"lines" binding:"required,min=1,max=20,dive"`
}

type prescriptionLineInput struct {
	PrescribedAs       string    `json:"prescribed_as" binding:"required"`
	ProductID          uuid.UUID `json:"product_id" binding:"required"`
	QuantityUnits      int64     `json:"quantity_units" binding:"required,min=1,max=100000"`
	DispensingsAllowed int       `json:"dispensings_allowed" binding:"required,min=1,max=12"`
}

// prescriptionLine is what was prescribed and what is left of it.
type prescriptionLine struct {
	LineNumber           int       `gorm:"column:line_number" json:"line_number"`
	PrescribedAs         string    `gorm:"column:prescribed_as" json:"prescribed_as"`
	ProductID            uuid.UUID `gorm:"column:product_id" json:"product_id"`
	QuantityUnits        int64     `gorm:"column:quantity_units" json:"quantity_units"`
	DispensingsAllowed   int       `gorm:"column:dispensings_allowed" json:"dispensings_allowed"`
	UnitsRemaining       int64     `gorm:"column:units_remaining" json:"units_remaining"`
	DispensingsRemaining int       `gorm:"column:dispensings_remaining" json:"dispensings_remaining"`
}

type prescription struct {
	ID                     uuid.UUID          `gorm:"column:id" json:"id"`
	OutletID               uuid.UUID          `gorm:"column:outlet_id" json:"outlet_id"`
	PrescriberName         string             `gorm:"column:prescriber_name" json:"prescriber_name"`
	PrescriberRegistration string             `gorm:"column:prescriber_registration" json:"prescriber_registration"`
	PrescriberCouncil      string             `gorm:"column:prescriber_council" json:"prescriber_council"`
	PrescribedOn           string             `gorm:"column:prescribed_on" json:"prescribed_on"`
	PatientName            string             `gorm:"column:patient_name" json:"patient_name"`
	PatientAddress         *string            `gorm:"column:patient_address" json:"patient_address"`
	PrescriberAddress      *string            `gorm:"column:prescriber_address" json:"prescriber_address"`
	RecordedBy             string             `gorm:"column:recorded_by" json:"recorded_by"`
	CreatedAt              time.Time          `gorm:"column:created_at" json:"created_at"`
	Lines                  []prescriptionLine `gorm:"-" json:"lines"`
}

func (prescription) TableName() string                 { return "pharmacy_prescriptions" }
func (r prescription) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const prescriptionColumns = "id,outlet_id,prescriber_name,prescriber_registration,prescriber_council,to_char(prescribed_on,'YYYY-MM-DD') AS prescribed_on,patient_name,patient_address,prescriber_address,recorded_by,created_at"

var registrationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/ .-]{0,39}$`)
var errFuturePrescription = errors.New("prescription dated after the outlet's today")

func prescriptionRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/prescriptions/:id", PermPrescriptionRecord, h.putPrescription)
	s.GET("/prescriptions/:id", PermPrescriptionRead, h.getPrescription)
	platform.ListRoute(s, "/prescriptions", PermPrescriptionRead, h.listPrescriptions)
}

// prescriptionText is trimmed-equal, free of control characters and within
// max runes; the table holds the same rule.
func prescriptionText(v string, max int) bool {
	n := len([]rune(v))
	return n >= 1 && n <= max && v == strings.TrimSpace(v) && !strings.ContainsFunc(v, unicode.IsControl)
}

func validPrescription(in prescriptionInput) bool {
	if _, err := time.Parse(time.DateOnly, in.PrescribedOn); err != nil {
		return false
	}
	if in.PatientAddress != nil && !prescriptionText(*in.PatientAddress, 500) {
		return false
	}
	if in.PrescriberAddress != nil && !prescriptionText(*in.PrescriberAddress, 500) {
		return false
	}
	for _, l := range in.Lines {
		if !prescriptionText(l.PrescribedAs, 200) || l.ProductID == uuid.Nil {
			return false
		}
	}
	return prescriptionText(in.PrescriberName, 200) && prescriptionText(in.PrescriberCouncil, 120) && prescriptionText(in.PatientName, 200) &&
		registrationPattern.MatchString(in.PrescriberRegistration) && in.PrescriberRegistration == strings.TrimSpace(in.PrescriberRegistration)
}

func (h *catalogueHandlers) putPrescription(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in prescriptionInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validPrescription(in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a prescription needs the prescriber's name, registration number and council, a date (YYYY-MM-DD), the patient's name and 1 to 20 prescribed lines, without leading or trailing spaces")
		return
	}
	address := ""
	if in.PatientAddress != nil {
		// \x1e keeps "no address" distinct from any address text.
		address = "\x1e" + *in.PatientAddress
	}
	parts := []string{in.PrescriberName, in.PrescriberRegistration, in.PrescriberCouncil, in.PrescribedOn, in.PatientName, address}
	if in.PrescriberAddress != nil {
		parts = append(parts, "\x1dprescriber", *in.PrescriberAddress)
	}
	for _, l := range in.Lines {
		parts = append(parts, "\x1dline", l.PrescribedAs, l.ProductID.String(), strconv.FormatInt(l.QuantityUnits, 10), strconv.Itoa(l.DispensingsAllowed))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var row prescription
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":prescription:"+id.String()); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, RecordedBy string }
		if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM pharmacy_prescriptions WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 && (existing[0].IntentSHA256 != digest || existing[0].RecordedBy != p.Subject) {
			return errStockIdentity
		}
		if len(existing) == 0 {
			today, err := outletLocalDate(tx, p.TenantID, outlet)
			if err != nil {
				return err
			}
			// Both are YYYY-MM-DD, so string order is date order.
			if in.PrescribedOn > today {
				return errFuturePrescription
			}
			created = true
			if err := tx.Exec(`INSERT INTO pharmacy_prescriptions (id,tenant_id,outlet_id,prescriber_name,prescriber_registration,prescriber_council,prescribed_on,patient_name,patient_address,prescriber_address,intent_sha256,recorded_by)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, id, p.TenantID, outlet, in.PrescriberName, in.PrescriberRegistration, in.PrescriberCouncil, in.PrescribedOn, in.PatientName, in.PatientAddress, in.PrescriberAddress, digest, p.Subject).Error; err != nil {
				return err
			}
			for i, l := range in.Lines {
				if err := tx.Exec(`INSERT INTO pharmacy_prescription_lines (tenant_id,outlet_id,prescription_id,line_number,prescribed_as,product_id,quantity_units,dispensings_allowed)
					VALUES (?,?,?,?,?,?,?,?)`, p.TenantID, outlet, id, i+1, l.PrescribedAs, l.ProductID, l.QuantityUnits, l.DispensingsAllowed).Error; err != nil {
					return err
				}
			}
		}
		var err error
		row, err = loadPrescription(tx, p.TenantID, outlet, id)
		return err
	})
	switch {
	case errors.Is(err, errFuturePrescription):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a prescription cannot be dated after today at this outlet")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this prescription was already recorded with different details; record a new prescription")
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, row)
	default:
		respond.OK(c, row)
	}
}

func loadPrescription(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (prescription, error) {
	var row prescription
	if err := tx.Select(prescriptionColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).First(&row).Error; err != nil {
		return row, err
	}
	row.Lines = []prescriptionLine{}
	err := tx.Raw(prescriptionLinesQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND l.prescription_id=? ORDER BY l.line_number`, tenant, outlet, id).Scan(&row.Lines).Error
	return row, err
}

// prescriptionLinesQuery reads each line with what is left of it: the units
// and the distinct sales that cited it, as the sale guard counts them.
const prescriptionLinesQuery = `SELECT l.line_number, l.prescribed_as, l.product_id, l.quantity_units, l.dispensings_allowed,
 l.quantity_units - COALESCE(d.sold,0) AS units_remaining, l.dispensings_allowed - COALESCE(d.sales,0) AS dispensings_remaining
 FROM pharmacy_prescription_lines l
 LEFT JOIN LATERAL (SELECT sum(s.quantity_units) AS sold, count(DISTINCT s.sale_id) AS sales FROM pharmacy_sale_lines s
  WHERE s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.prescription_id=l.prescription_id AND s.prescription_line=l.line_number) d ON true`

func (h *catalogueHandlers) getPrescription(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var row prescription
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) (err error) {
		row, err = loadPrescription(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *catalogueHandlers) listPrescriptions(c *gin.Context, params pagination.Params) ([]prescription, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []prescription
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(prescriptionColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
