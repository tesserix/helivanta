package pharmacy

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type dispense struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	Medication  string     `json:"medication"`
	Status      string     `gorm:"default:pending" json:"status"`
	DispensedAt *time.Time `json:"dispensed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (dispense) TableName() string { return "pharmacy_dispenses" }

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (d dispense) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

type dispenseHandlers struct {
	db *tenantdb.DB
}

// list returns one page of this tenant's dispenses, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint.
func (h *dispenseHandlers) list(c *gin.Context, p pagination.Params) ([]dispense, error) {
	principal, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	// The queue splits on the server (#1258): filtering a page in the
	// browser would hide pending rows behind dispensed ones. An unknown
	// status is refused, not ignored, so a typo never returns everything.
	status := c.Query("status")
	if status != "" && status != "pending" && status != "dispensed" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "status must be pending or dispensed")
		return nil, nil
	}
	var rows []dispense
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx, p)
		if status != "" {
			q = q.Where("status = ?", status)
		}
		return q.Find(&rows).Error
	})
	return rows, err
}

// dispensedRow pages by when the medicine was handed over (#1295 D8). Its
// list is scoped to status 'dispensed', and the pharmacy_dispenses_dispensed_at
// constraint requires dispensed_at with that status, so DispensedAt is never nil here.
// A defined type rather than an embedding: GORM does not scan through an
// embedded unexported struct.
type dispensedRow dispense

func (dispensedRow) TableName() string                 { return "pharmacy_dispenses" }
func (d dispensedRow) PageKey() (time.Time, uuid.UUID) { return *d.DispensedAt, d.ID }

// dispenseStatus is the counted Waiting / Dispensed filter. The pharmacy
// tenant check runs first, as requirePharmacyTenant does on the other
// dispense routes; a refusal has already been answered.
//
// Waiting takes ?q=, a case-insensitive patient-name search (#1295 D9), so
// the patient at the counter can be found in a long queue. LIKE wildcards
// in q are taken literally. Dispensed history is unbounded, so a search
// there is refused rather than silently ignored.
func dispenseStatus(status string) platform.CountedListHandler {
	return func(c *gin.Context) (platform.Scope, bool) {
		if _, _, ok := pharmacyPrincipal(c); !ok {
			return nil, false
		}
		q := strings.TrimSpace(c.Query("q"))
		if q != "" && status != "pending" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "search is available on waiting dispenses only")
			return nil, false
		}
		if len([]rune(q)) > 100 {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "search must be at most 100 characters")
			return nil, false
		}
		return func(tx *gorm.DB) *gorm.DB {
			tx = tx.Where("status = ?", status)
			if q != "" {
				tx = tx.Where(`patient_name ILIKE ? ESCAPE '\'`, "%"+likeLiteral.Replace(q)+"%")
			}
			return tx
		}, true
	}
}

var likeLiteral = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// fulfil is the retired free-text dispense (#1680). It recorded a hand-over
// with no product, stock, schedule, prescription or register check; the
// counter sale against an OPD encounter is the only way to dispense now.
// After the shared session and pharmacy-tenant checks, it refuses before
// reading the row: failing closed on every row state means no pending,
// dispensed or foreign row can reach a write or reveal that it exists.
func (h *dispenseHandlers) fulfil(c *gin.Context) {
	if _, _, ok := pharmacyPrincipal(c); !ok {
		return
	}
	respond.Error(c, http.StatusGone, "dispense_retired",
		"Free-text dispensing is retired. Record a counter sale against the patient's OPD encounter.")
}
