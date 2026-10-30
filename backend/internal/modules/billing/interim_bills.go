package billing

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	errDischargeOrdered = errors.New("discharge is ordered")
	errNotDischarged    = errors.New("discharge is not ordered")
	errFinalUnpriced    = errors.New("bed charging is not set up or a bed day is not priced")
	errBedDaysPending   = errors.New("bed days are still being counted")
	errAccountClosed    = errors.New("the account is closed")
	errNothingNew       = errors.New("nothing new since the last bill")
	errNoBill           = errors.New("bill not found")
)

// interimBill is the account as it stood when the bill was issued. Its
// totals derive from its lines, which name append-only entries, so they never
// change.
type interimBill struct {
	ID              uuid.UUID `json:"id"`
	AdmissionID     uuid.UUID `json:"admission_id"`
	HospitalID      uuid.UUID `json:"hospital_id"`
	PatientID       uuid.UUID `json:"patient_id"`
	Number          int       `json:"number"`
	Kind            string    `json:"kind"`
	IssuedBy        string    `json:"issued_by"`
	IssuedAt        time.Time `json:"issued_at"`
	UnpricedBedDays int       `json:"unpriced_bed_days"`
	LineCount       int64     `json:"line_count"`
	NewCount        int64     `json:"new_count"`
	Totals          []total   `json:"totals" gorm:"-"`
	Sources         []source  `json:"sources" gorm:"-"`
}

func (interimBill) TableName() string                 { return "billing_interim_bills" }
func (b interimBill) PageKey() (time.Time, uuid.UUID) { return b.IssuedAt, b.ID }

// billLine is an entry as a bill shows it. Its fields are repeated, not
// embedded: gorm cannot fill an unexported embedded struct.
type billLine struct {
	ID              uuid.UUID `json:"id"`
	Kind            string    `json:"kind"`
	SourceModule    string    `json:"source_module"`
	SourceReference uuid.UUID `json:"source_reference"`
	SourceDocument  string    `json:"source_document"`
	ChargeReference uuid.UUID `json:"charge_reference"`
	AmountMinor     int64     `json:"amount_minor"`
	Currency        string    `json:"currency"`
	OccurredAt      time.Time `json:"occurred_at"`
	RecordedAt      time.Time `json:"recorded_at"`
	New             bool      `json:"new"`
}

func (b billLine) PageKey() (time.Time, uuid.UUID) { return b.RecordedAt, b.ID }

const billColumns = "b.id,b.admission_id,b.hospital_id,a.patient_id,b.number,b.kind,b.issued_by,b.issued_at,b.unpriced_bed_days," +
	"(SELECT count(*) FROM billing_interim_bill_lines l WHERE l.tenant_id=b.tenant_id AND l.admission_id=b.admission_id AND l.number=b.number) AS line_count," +
	"(SELECT count(*) FROM billing_interim_bill_lines l WHERE l.tenant_id=b.tenant_id AND l.admission_id=b.admission_id AND l.number=b.number AND l.new) AS new_count"

const billFrom = "billing_interim_bills b JOIN billing_admission_accounts a ON a.tenant_id=b.tenant_id AND a.admission_id=b.admission_id"

func (h *handlers) issueInterimBill(c *gin.Context) { h.issueBill(c, "interim") }
func (h *handlers) issueFinalBill(c *gin.Context)   { h.issueBill(c, "final") }

// issueBill freezes the account into the next numbered bill. An interim bill
// is for an open account before discharge is ordered; a final bill for one
// whose discharge is ordered or whose patient has left, with every bed day
// priced. The database holds the same rules.
func (h *handlers) issueBill(c *gin.Context, kind string) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return
	}
	var out interimBill
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// The bed-day lock: never a reconcile half seen, and issues one at a time.
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		var status []string
		if err := tx.Raw(`SELECT status FROM billing_admission_accounts WHERE tenant_id=? AND hospital_id=? AND admission_id=? FOR SHARE`, p.TenantID, hospital, admission).Scan(&status).Error; err != nil {
			return err
		}
		if len(status) == 0 {
			return errNoAccount
		}
		var ordered struct{ Ordered, Configured, Settled bool }
		if err := tx.Raw(`SELECT billing_discharge_ordered(?,?) AS ordered, EXISTS (SELECT 1 FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=?) AS configured,
 billing_bed_days_settled(?,?,?) AS settled`,
			p.TenantID, admission, p.TenantID, hospital, p.TenantID, hospital, admission).Scan(&ordered).Error; err != nil {
			return err
		}
		switch {
		case kind == "interim" && status[0] != "open":
			return errAccountClosed
		case kind == "interim" && ordered.Ordered:
			return errDischargeOrdered
		case kind == "final" && status[0] == "open" && !ordered.Ordered:
			return errNotDischarged
		case kind == "final" && !ordered.Configured:
			return errFinalUnpriced
		case kind == "final" && !ordered.Settled:
			// A day the census or a late stay event may still add: the bill
			// waits for it rather than going out without it.
			return errBedDaysPending
		}
		var at struct {
			Previous, PreviousUnpriced, Unpriced, Fresh int
		}
		if err := tx.Raw(`SELECT COALESCE(p.number,0) AS previous, COALESCE(p.unpriced_bed_days,0) AS previous_unpriced,
 (SELECT count(*) FROM billing_bed_days d WHERE d.tenant_id=? AND d.admission_id=? AND d.voided_at IS NULL AND d.priced_at IS NULL) AS unpriced,
 (SELECT count(*) FROM billing_account_entries e WHERE e.tenant_id=? AND e.admission_id=? AND NOT EXISTS
   (SELECT 1 FROM billing_interim_bill_lines l WHERE l.tenant_id=e.tenant_id AND l.admission_id=e.admission_id AND l.number=p.number AND l.id=e.id)) AS fresh
 FROM (SELECT 1) one LEFT JOIN LATERAL (SELECT number,unpriced_bed_days FROM billing_interim_bills WHERE tenant_id=? AND admission_id=? ORDER BY number DESC LIMIT 1) p ON true`,
			p.TenantID, admission, p.TenantID, admission, p.TenantID, admission).Scan(&at).Error; err != nil {
			return err
		}
		// A final bill leaves out nothing it could not price.
		if kind == "final" && at.Unpriced > 0 {
			return errFinalUnpriced
		}
		// Nothing the previous bill lacks: refused, which also absorbs a repeated
		// click. A first final bill after interim ones is always new: it is final.
		var lastKind []string
		if err := tx.Raw(`SELECT kind FROM billing_interim_bills WHERE tenant_id=? AND admission_id=? ORDER BY number DESC LIMIT 1`, p.TenantID, admission).Scan(&lastKind).Error; err != nil {
			return err
		}
		sameKind := len(lastKind) == 0 || lastKind[0] == kind || kind == "interim"
		if at.Fresh == 0 && at.Unpriced == at.PreviousUnpriced && sameKind {
			return errNothingNew
		}
		number := at.Previous + 1
		if err := tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,kind,id,issued_by,unpriced_bed_days) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, admission, number, kind, uuid.Must(uuid.NewV7()), p.Subject, at.Unpriced).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO billing_interim_bill_lines (tenant_id,hospital_id,admission_id,number,id,new)
 SELECT e.tenant_id,?,e.admission_id,?,e.id,NOT EXISTS (SELECT 1 FROM billing_interim_bill_lines l WHERE l.tenant_id=e.tenant_id AND l.admission_id=e.admission_id AND l.number=? AND l.id=e.id)
 FROM billing_account_entries e WHERE e.tenant_id=? AND e.admission_id=?`, hospital, number, at.Previous, p.TenantID, admission).Error; err != nil {
			return err
		}
		out, err = loadBill(tx, p.TenantID, hospital, admission, number, "")
		return err
	})
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errAccountClosed):
		respond.Error(c, http.StatusConflict, "account_closed", "the admission is released; issue the final bill")
	case errors.Is(err, errDischargeOrdered), errors.As(err, &pg) && pg.ConstraintName == "billing_interim_bill_open":
		respond.Error(c, http.StatusConflict, "discharge_ordered", "discharge is ordered; issue the final bill")
	case errors.Is(err, errNotDischarged), errors.As(err, &pg) && pg.ConstraintName == "billing_final_bill_discharge":
		respond.Error(c, http.StatusConflict, "discharge_not_ordered", "a final bill is issued once a doctor orders discharge or the patient has left")
	case errors.Is(err, errBedDaysPending), errors.As(err, &pg) && pg.ConstraintName == "billing_final_bill_settled":
		respond.Error(c, http.StatusConflict, "bed_days_pending", "bed days are still being counted; try again in a few minutes")
	case errors.Is(err, errFinalUnpriced), errors.As(err, &pg) && pg.ConstraintName == "billing_final_bill_priced":
		respond.Error(c, http.StatusConflict, "bed_days_not_priced", "a final bill needs bed charging set up and a rate for every bed day")
	case errors.Is(err, errNothingNew):
		respond.Error(c, http.StatusConflict, "nothing_new", "nothing has been charged or reversed since the last bill")
	case err != nil:
		failure(c, err)
	default:
		respond.Created(c, out)
	}
}

// loadBill reads one bill; kind "" is either kind.
func loadBill(tx *gorm.DB, tenant string, hospital, admission uuid.UUID, number int, kind string) (interimBill, error) {
	var rows []interimBill
	if err := tx.Table(billFrom).Select(billColumns).Where("b.tenant_id=? AND b.hospital_id=? AND b.admission_id=? AND b.number=? AND (?='' OR b.kind=?)", tenant, hospital, admission, number, kind, kind).Scan(&rows).Error; err != nil {
		return interimBill{}, err
	}
	if len(rows) != 1 {
		return interimBill{}, errNoBill
	}
	b := rows[0]
	lines := `FROM billing_interim_bill_lines l JOIN billing_account_entries e ON e.tenant_id=l.tenant_id AND e.admission_id=l.admission_id AND e.id=l.id
 WHERE l.tenant_id=? AND l.admission_id=? AND l.number=?`
	b.Totals = []total{}
	if err := tx.Raw(`SELECT e.currency, COALESCE(sum(e.amount_minor) FILTER (WHERE e.kind='charge'),0) AS charged_minor,
 COALESCE(sum(e.amount_minor) FILTER (WHERE e.kind='reversal'),0) AS reversed_minor `+lines+` GROUP BY e.currency ORDER BY e.currency`, tenant, admission, number).Scan(&b.Totals).Error; err != nil {
		return interimBill{}, err
	}
	for i := range b.Totals {
		b.Totals[i].OutstandingMinor = b.Totals[i].ChargedMinor - b.Totals[i].ReversedMinor
	}
	b.Sources = []source{}
	if err := tx.Raw(`SELECT e.source_module,e.currency,sum(CASE e.kind WHEN 'charge' THEN e.amount_minor ELSE -e.amount_minor END) AS outstanding_minor `+lines+`
 GROUP BY e.source_module,e.currency ORDER BY e.source_module,e.currency`, tenant, admission, number).Scan(&b.Sources).Error; err != nil {
		return interimBill{}, err
	}
	return b, nil
}

func billNumber(c *gin.Context) (uuid.UUID, int, bool) {
	admission, err := uuid.Parse(c.Param("admission"))
	// The column is an integer: a larger number is no bill, not an error.
	number, nerr := strconv.ParseInt(c.Param("number"), 10, 32)
	return admission, int(number), err == nil && nerr == nil && number >= 1
}

// bill answers one bill: the interim routes only interim ones, the bill
// routes either kind (#197 slice 4).
func (h *handlers) bill(kind string) func(c *gin.Context) {
	return func(c *gin.Context) { h.oneBill(c, kind) }
}

func (h *handlers) oneBill(c *gin.Context, kind string) {
	admission, number, ok := billNumber(c)
	if !ok {
		respond.NotFound(c, "bill")
		return
	}
	var out interimBill
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = loadBill(tx, p.TenantID, hospital, admission, number, kind)
		return err
	})
	if errors.Is(err, errNoBill) {
		respond.NotFound(c, "bill")
		return
	}
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}

// bills lists the admission's bills of a kind ("" for both), newest first,
// without totals: each bill's own route has them.
func (h *handlers) bills(kind string) func(*gin.Context, pagination.Params) ([]interimBill, error) {
	return func(c *gin.Context, params pagination.Params) ([]interimBill, error) {
		return h.listBills(c, params, kind)
	}
}

func (h *handlers) listBills(c *gin.Context, params pagination.Params, kind string) ([]interimBill, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []interimBill
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		q := tx.Table("(SELECT "+billColumns+" FROM "+billFrom+" WHERE b.tenant_id=? AND b.hospital_id=? AND b.admission_id=? AND (?='' OR b.kind=?)) AS bills", p.TenantID, hospital, admission, kind, kind)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "issued_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].Totals, rows[i].Sources = []total{}, []source{}
	}
	return rows, nil
}

func (h *handlers) billLines(kind string) func(*gin.Context, pagination.Params) ([]billLine, error) {
	return func(c *gin.Context, params pagination.Params) ([]billLine, error) {
		return h.listBillLines(c, params, kind)
	}
}

func (h *handlers) listBillLines(c *gin.Context, params pagination.Params, kind string) ([]billLine, error) {
	admission, number, ok := billNumber(c)
	if !ok {
		respond.NotFound(c, "bill")
		return nil, nil
	}
	var rows []billLine
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_interim_bills").Where("tenant_id=? AND hospital_id=? AND admission_id=? AND number=? AND (?='' OR kind=?)", p.TenantID, hospital, admission, number, kind, kind).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoBill
		}
		q := tx.Table(`(SELECT e.*,l.new FROM billing_interim_bill_lines l JOIN billing_account_entries e ON e.tenant_id=l.tenant_id AND e.admission_id=l.admission_id AND e.id=l.id
 WHERE l.tenant_id=? AND l.admission_id=? AND l.number=?) AS lines`, p.TenantID, admission, number)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if errors.Is(err, errNoBill) {
		respond.NotFound(c, "bill")
		return nil, nil
	}
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
