package pharmacy

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

type wardIndentQueueView struct {
	wardIndentView
	OccupancyID uuid.UUID `json:"occupancy_id"`
}

type wardIndentCloseInput struct {
	Reason *string `json:"reason" binding:"omitempty,min=5,max=200"`
}

func wardIndentPharmacyRoutes(s *platform.Router, h *catalogueHandlers) {
	platform.ListRoute(s, "/ward-indents", PermIndentFulfil, h.listFulfilmentIndents)
	s.GET("/ward-indents/:id", PermIndentFulfil, h.getFulfilmentIndent)
	s.PUT("/ward-indents/:id/closure", PermIndentFulfil, h.putWardIndentClosure)
}

func readFulfilmentIndent(tx *gorm.DB, tenant string, outlet, id uuid.UUID, lock bool) (wardIndentQueueView, error) {
	var ids struct{ HospitalID, DepartmentID, OccupancyID uuid.UUID }
	if err := tx.Table("pharmacy_ward_indents").Select("hospital_id,department_id,occupancy_id").Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Take(&ids).Error; err != nil {
		return wardIndentQueueView{}, err
	}
	view, err := readWardIndent(tx, tenant, outlet, ids.HospitalID, ids.DepartmentID, id, lock)
	return wardIndentQueueView{wardIndentView: view, OccupancyID: ids.OccupancyID}, err
}

func (h *catalogueHandlers) listFulfilmentIndents(c *gin.Context, page pagination.Params) ([]wardIndentQueueView, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	status := c.DefaultQuery("status", "open")
	if status != "open" && status != "closed" && status != "cancelled" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Filter by open, closed or cancelled.")
		return nil, nil
	}
	rows := []wardIndentQueueView{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		query := tx.Table("pharmacy_ward_indents").Select(wardIndentColumns+",occupancy_id").Where("tenant_id=? AND outlet_id=? AND status=?", p.TenantID, outlet, status)
		if page.After != nil {
			query = query.Where("(raised_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Order("raised_at DESC,id DESC").Limit(page.Limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indents.fulfilment_read", "pharmacy_outlet", outlet)
	})
	if err != nil {
		stockError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) getFulfilmentIndent(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var view wardIndentQueueView
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) (err error) {
		if view, err = readFulfilmentIndent(tx, p.TenantID, outlet, id, false); err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indent.fulfilment_read", "pharmacy_ward_indent", id)
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, view)
}

// putWardIndentClosure closes an indent: fully issued with no reason, or short with a reason while units are outstanding.
func (h *catalogueHandlers) putWardIndentClosure(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in wardIndentCloseInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if in.Reason != nil && !prescriptionText(*in.Reason, 200) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Give a reason of 5 to 200 characters.")
		return
	}
	var view wardIndentQueueView
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":ward-indent:"+id.String()); err != nil {
			return err
		}
		current, err := readFulfilmentIndent(tx, p.TenantID, outlet, id, true)
		if err != nil {
			return err
		}
		if current.Status == "closed" {
			view = current
			return nil
		}
		if current.Status != "open" {
			return errWardIndentClosed
		}
		outstanding := false
		for _, l := range current.Lines {
			outstanding = outstanding || l.IssuedUnits < l.RequestedUnits
		}
		if outstanding != (in.Reason != nil) {
			return errWardIndentReason{outstanding}
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		var closedAt time.Time
		if err := tx.Raw(`UPDATE pharmacy_ward_indents SET status='closed',version=2,close_reason=?,closed_by=? WHERE tenant_id=? AND outlet_id=? AND id=? AND status='open' RETURNING closed_at`,
			in.Reason, p.Subject, p.TenantID, outlet, id).Scan(&closedAt).Error; err != nil {
			return err
		}
		if closedAt.IsZero() {
			return errWardIndentClosed
		}
		if err := h.publishWardIndent(tx, p.TenantID, pharmacycontract.SubjectWardIndentClosed, "WardIndentClosed", pharmacycontract.WardIndentClosedData{
			IndentID: id.String(), OutletID: outlet.String(), Status: "closed", ClosedAt: closedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if err := wardIndentAudit(tx, p, "pharmacy.ward_indent.closed", "pharmacy_ward_indent", id); err != nil {
			return err
		}
		view, err = readFulfilmentIndent(tx, p.TenantID, outlet, id, false)
		return err
	})
	var reason errWardIndentReason
	switch {
	case errors.Is(err, errWardIndentClosed):
		respond.Error(c, http.StatusConflict, "indent_closed", "This indent is no longer open.")
	case errors.As(err, &reason) && reason.outstanding:
		respond.Error(c, http.StatusBadRequest, "reason_required", "Units are still outstanding. Give the short-supply reason to close it.")
	case errors.As(err, &reason):
		respond.Error(c, http.StatusBadRequest, "reason_not_allowed", "Everything was issued, so close it without a reason.")
	case errors.Is(err, errWardIndentEvidence):
		respond.Error(c, http.StatusServiceUnavailable, "audit_unavailable", "Evidence could not be recorded; nothing was saved.")
	case err != nil:
		stockError(c, err)
	default:
		respond.OK(c, view)
	}
}

var errWardIndentClosed = errors.New("ward indent is no longer open")

type errWardIndentReason struct{ outstanding bool }

func (e errWardIndentReason) Error() string {
	return "ward indent close reason does not match outstanding units"
}

// issueWardIndent records each sale line as an issue of the indent's matching product; the database re-checks every rule.
func issueWardIndent(tx *gorm.DB, p authn.Principal, outlet, sale, indent uuid.UUID, lines []saleLine) error {
	var rows []struct {
		LineNumber                          int
		ProductID, HospitalID, DepartmentID uuid.UUID
	}
	if err := tx.Raw(`SELECT line_number,product_id,hospital_id,department_id FROM pharmacy_ward_indent_lines WHERE tenant_id=? AND outlet_id=? AND indent_id=?`, p.TenantID, outlet, indent).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return errNotSellable{"that ward indent is not open at this outlet"}
	}
	for _, l := range lines {
		found := false
		for _, r := range rows {
			if r.ProductID != l.ProductID {
				continue
			}
			found = true
			if err := tx.Exec(`INSERT INTO pharmacy_ward_indent_issues(tenant_id,outlet_id,sale_id,sale_line,indent_id,line_number,hospital_id,department_id,quantity_units,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,?)`,
				p.TenantID, outlet, sale, l.LineNumber, indent, r.LineNumber, r.HospitalID, r.DepartmentID, l.QuantityUnits, p.Subject).Error; err != nil {
				return err
			}
		}
		if !found {
			return errNotSellable{"a ward indent issue sells only the products on the indent"}
		}
	}
	return nil
}

// wardIndentIssueRefused answers a database refusal of an issue and reports whether it did.
func wardIndentIssueRefused(c *gin.Context, err error) bool {
	reasons := map[string]string{
		"pharmacy_ward_indent_open":           "that ward indent is no longer open",
		"pharmacy_ward_indent_issue_sale":     "an issue must be a new admission-account sale for the indent's patient",
		"pharmacy_ward_indent_issue_line":     "an issue line must be the indented product in the quantity sold",
		"pharmacy_ward_indent_over_issue":     "that is more than the indent still needs",
		"pharmacy_ward_indent_issue_complete": "every line of an issue sale must be an indent line",
	}
	for name, reason := range reasons {
		if violated(err, name) {
			respond.Error(c, http.StatusConflict, "not_sellable", reason)
			return true
		}
	}
	return false
}
