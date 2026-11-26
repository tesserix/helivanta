package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var wardIndentLocation = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,19}$`)

type wardIndentLineView struct {
	LineNumber     int       `json:"line_number"`
	ProductID      uuid.UUID `json:"product_id"`
	BrandName      string    `json:"brand_name"`
	GenericName    string    `json:"generic_name"`
	RequestedUnits int64     `json:"requested_units"`
	IssuedUnits    int64     `json:"issued_units"`
	ReturnedUnits  int64     `json:"returned_units"`
}

type wardIndentView struct {
	ID             uuid.UUID            `json:"id"`
	OutletID       uuid.UUID            `json:"outlet_id"`
	HospitalID     uuid.UUID            `json:"hospital_id"`
	DepartmentID   uuid.UUID            `json:"department_id"`
	Kind           string               `json:"kind"`
	WardCode       string               `json:"ward_code"`
	RoomCode       string               `json:"room_code"`
	BedCode        string               `json:"bed_code"`
	HospitalName   string               `json:"hospital_name"`
	DepartmentName string               `json:"department_name"`
	LineCount      int                  `json:"line_count"`
	Status         string               `json:"status"`
	Version        int64                `json:"version"`
	CloseReason    *string              `json:"close_reason"`
	RaisedAt       time.Time            `json:"raised_at"`
	ClosedAt       *time.Time           `json:"closed_at"`
	Lines          []wardIndentLineView `json:"lines,omitempty" gorm:"-"`
}

func (wardIndentView) TableName() string { return "pharmacy_ward_indents" }

func (v wardIndentView) PageKey() (time.Time, uuid.UUID) { return v.RaisedAt, v.ID }

const wardIndentColumns = "id,outlet_id,hospital_id,department_id,kind,ward_code,room_code,bed_code,hospital_name,department_name,line_count,status,version,close_reason,raised_at,closed_at"

type wardIndentOutletChoice struct {
	ID           uuid.UUID `json:"id"`
	PharmacyID   uuid.UUID `json:"pharmacy_id"`
	PharmacyName string    `json:"pharmacy_name"`
	Name         string    `json:"name"`
	Code         string    `json:"code"`
	CreatedAt    time.Time `json:"-"`
}

func (r wardIndentOutletChoice) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type wardIndentOccupant struct {
	OccupancyID uuid.UUID `json:"occupancy_id"`
	WardCode    string    `json:"ward_code"`
	RoomCode    string    `json:"room_code"`
	BedCode     string    `json:"bed_code"`
}

func wardIndentRoutes(g *platform.Router, h *catalogueHandlers) {
	root := g.Group("/hospitals/:hospital/departments/:department")
	platform.ResourceListRoute(root, "/ward-indent-outlet-choices", authz.Public, 20, h.listWardIndentOutlets)
	platform.ResourceListRoute(root, "/outlets/:outlet/ward-indent-products", authz.Public, 50, h.listWardIndentProducts)
	root.GET("/outlets/:outlet/ward-indent-occupant", authz.Public, h.getWardIndentOccupant)
	platform.ResourceListRoute(root, "/outlets/:outlet/ward-indents", authz.Public, 50, h.listWardIndents)
	root.GET("/outlets/:outlet/ward-indents/:indent", authz.Public, h.getWardIndent)
	root.PUT("/outlets/:outlet/ward-indents/:indent", authz.Public, h.putWardIndent)
	root.PUT("/outlets/:outlet/ward-indents/:indent/cancellation", authz.Public, h.putWardIndentCancellation)
}

// wardIndentRequest validates the department and outlet in the path and the exact query keys a route accepts.
func wardIndentRequest(c *gin.Context, outlet bool, query ...string) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	hospital, hospitalErr := uuid.Parse(c.Param("hospital"))
	_, departmentOK := operatingStockID(c.Param("department"))
	if hospitalErr != nil || hospital == uuid.Nil || hospital.Variant() != uuid.RFC4122 || hospital.String() != c.Param("hospital") || !departmentOK {
		respond.NotFound(c, "ward indent resource")
		return p, uuid.Nil, false
	}
	outletID := uuid.Nil
	if outlet {
		var ok bool
		if outletID, ok = operatingStockID(c.Param("outlet")); !ok || outletID.String() != c.Param("outlet") {
			respond.NotFound(c, "ward indent resource")
			return p, uuid.Nil, false
		}
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		respond.Error(c, 400, "invalid_request", "Use a valid ward indent request.")
		return p, uuid.Nil, false
	}
	for key, found := range values {
		if len(found) != 1 || found[0] == "" || !slices.Contains(query, key) {
			respond.Error(c, 400, "invalid_request", "Use only the documented ward indent parameters.")
			return p, uuid.Nil, false
		}
	}
	return p, outletID, true
}

func wardIndentID(c *gin.Context) (uuid.UUID, bool) {
	id, ok := operatingStockID(c.Param("indent"))
	if !ok || id.String() != c.Param("indent") {
		respond.NotFound(c, "ward indent")
		return uuid.Nil, false
	}
	return id, true
}

func wardIndentAudit(tx *gorm.DB, p authn.Principal, action, resourceType string, resource any) error {
	if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,?,?,?)`, p.TenantID, p.Subject, action, resourceType, resource).Error; err != nil {
		return errors.Join(errWardIndentEvidence, err)
	}
	return nil
}

func (h *catalogueHandlers) listWardIndentOutlets(c *gin.Context, page pagination.Params) ([]wardIndentOutletChoice, error) {
	p, _, ok := wardIndentRequest(c, false, "limit", "cursor")
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	rows := []wardIndentOutletChoice{}
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), uuid.Nil, PermIndentRead, false, func(tx *gorm.DB, use wardIndentUse) error {
		query := tx.Table("pharmacy_outlets AS o").Select("o.id,o.pharmacy_id,p.name AS pharmacy_name,o.name,o.code,o.created_at").
			Joins("JOIN pharmacy_organizations p ON p.tenant_id=o.tenant_id AND p.id=o.pharmacy_id").
			Where("o.tenant_id=? AND o.country_code='IN' AND pharmacy_ward_indent_affiliated(o.tenant_id,o.id,?)", p.TenantID, use.hospital)
		if page.After != nil {
			query = query.Where("(o.created_at,o.id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Order("o.created_at DESC,o.id DESC").Limit(page.Limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indent_outlet_choices.read", "hospital", use.hospital)
	})
	if err != nil {
		wardIndentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) listWardIndentProducts(c *gin.Context, page pagination.Params) ([]receivingProduct, error) {
	p, outlet, ok := wardIndentRequest(c, true, "limit", "cursor", "q")
	if !ok {
		return nil, nil
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if len(q) > 200 {
		respond.Error(c, 400, "invalid_request", "Search text is too long.")
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	rows := []receivingProduct{}
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRead, false, func(tx *gorm.DB, use wardIndentUse) error {
		if err := tenantdb.ApplyKeyset(tx, page).Select(receivingProductColumns).Scopes(productFilters("active", q)).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error; err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indent_products.read", "pharmacy_outlet", outlet)
	})
	if err != nil {
		wardIndentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) getWardIndentOccupant(c *gin.Context) {
	p, outlet, ok := wardIndentRequest(c, true, "bed_code", "room_code")
	if !ok {
		return
	}
	bed, room := c.Query("bed_code"), c.Query("room_code")
	if (bed == "") == (room == "") || !wardIndentLocation.MatchString(bed+room) {
		respond.Error(c, 400, "invalid_request", "Look up exactly one bed or room code.")
		return
	}
	column, code := "bed_code", bed
	if room != "" {
		column, code = "room_code", room
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var found []wardIndentOccupant
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRead, false, func(tx *gorm.DB, use wardIndentUse) error {
		var affiliated bool
		if err := tx.Raw(`SELECT pharmacy_ward_indent_affiliated(?,?,?)`, p.TenantID, outlet, use.hospital).Scan(&affiliated).Error; err != nil {
			return err
		}
		if !affiliated {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Table("pharmacy_bed_occupancies").Select("occupancy_id,ward_code,room_code,bed_code").
			Where("tenant_id=? AND hospital_id=? AND started_at IS NOT NULL AND ended_at IS NULL AND "+column+"=?", p.TenantID, use.hospital, code).Limit(2).Scan(&found).Error; err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indent_occupant.read", "hospital", use.hospital)
	})
	if err != nil {
		wardIndentError(c, err)
		return
	}
	switch len(found) {
	case 0:
		respond.Error(c, 404, "no_occupant", "No patient is in that bed or room.")
	case 1:
		respond.OK(c, found[0])
	default:
		respond.Error(c, 409, "ambiguous_occupant", "More than one patient is in that room. Look up the bed instead.")
	}
}

func readWardIndent(tx *gorm.DB, tenant string, outlet, hospital, department, id uuid.UUID, lock bool) (wardIndentView, error) {
	var view wardIndentView
	query := tx.Select(wardIndentColumns).Where("tenant_id=? AND outlet_id=? AND hospital_id=? AND department_id=? AND id=?", tenant, outlet, hospital, department, id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&view).Error; err != nil {
		return view, err
	}
	err := tx.Raw(`SELECT l.line_number,l.product_id,COALESCE(p.brand_name,'')AS brand_name,COALESCE(p.generic_name,'')AS generic_name,l.requested_units,
 COALESCE((SELECT sum(i.quantity_units)FROM pharmacy_ward_indent_issues i WHERE i.tenant_id=l.tenant_id AND i.outlet_id=l.outlet_id AND i.indent_id=l.indent_id AND i.line_number=l.line_number),0)::bigint AS issued_units,
 COALESCE((SELECT sum(n.quantity_units)FROM pharmacy_ward_indent_issues i JOIN pharmacy_credit_note_lines n ON n.tenant_id=i.tenant_id AND n.outlet_id=i.outlet_id AND n.sale_id=i.sale_id AND n.sale_line=i.sale_line
  WHERE i.tenant_id=l.tenant_id AND i.outlet_id=l.outlet_id AND i.indent_id=l.indent_id AND i.line_number=l.line_number),0)::bigint AS returned_units
 FROM pharmacy_ward_indent_lines l LEFT JOIN pharmacy_catalogue_products p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.product_id
 WHERE l.tenant_id=? AND l.outlet_id=? AND l.indent_id=? ORDER BY l.line_number`, tenant, outlet, id).Scan(&view.Lines).Error
	return view, err
}

func (h *catalogueHandlers) listWardIndents(c *gin.Context, page pagination.Params) ([]wardIndentView, error) {
	p, outlet, ok := wardIndentRequest(c, true, "limit", "cursor", "status")
	if !ok {
		return nil, nil
	}
	status := c.Query("status")
	if status != "" && status != "open" && status != "closed" && status != "cancelled" {
		respond.Error(c, 400, "invalid_request", "Filter by open, closed or cancelled.")
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	rows := []wardIndentView{}
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRead, false, func(tx *gorm.DB, use wardIndentUse) error {
		query := tx.Select(wardIndentColumns).Where("tenant_id=? AND outlet_id=? AND hospital_id=? AND department_id=?", p.TenantID, outlet, use.hospital, use.department)
		if status != "" {
			query = query.Where("status=?", status)
		}
		if page.After != nil {
			query = query.Where("(raised_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Order("raised_at DESC,id DESC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indents.read", "pharmacy_outlet", outlet)
	})
	if err != nil {
		wardIndentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) getWardIndent(c *gin.Context) {
	p, outlet, ok := wardIndentRequest(c, true)
	if !ok {
		return
	}
	id, ok := wardIndentID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var view wardIndentView
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRead, false, func(tx *gorm.DB, use wardIndentUse) (err error) {
		if view, err = readWardIndent(tx, p.TenantID, outlet, use.hospital, use.department, id, false); err != nil {
			return err
		}
		return wardIndentAudit(tx, p, "pharmacy.ward_indent.read", "pharmacy_ward_indent", id)
	})
	if err != nil {
		wardIndentError(c, err)
		return
	}
	respond.OK(c, view)
}

func (h *catalogueHandlers) putWardIndent(c *gin.Context) {
	p, outlet, ok := wardIndentRequest(c, true)
	if !ok {
		return
	}
	id, ok := wardIndentID(c)
	if !ok {
		return
	}
	var in WardIndentInput
	if !decodeStockCommand(c, &in) {
		return
	}
	products := map[uuid.UUID]bool{}
	for _, line := range in.Lines {
		if line.ProductID == uuid.Nil || products[line.ProductID] {
			respond.Error(c, 400, "invalid_request", "Request each product once.")
			return
		}
		products[line.ProductID] = true
	}
	if in.OccupancyID == uuid.Nil {
		respond.Error(c, 400, "invalid_request", "Choose the patient's bed.")
		return
	}
	hash, err := stocktakeHash(p.Subject, struct {
		Hospital, Department string
		Outlet, Indent       uuid.UUID
		Input                WardIndentInput
	}{c.Param("hospital"), c.Param("department"), outlet, id, in})
	if err != nil {
		wardIndentError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var view wardIndentView
	err = h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRaise, true, func(tx *gorm.DB, use wardIndentUse) error {
		if err := stockLock(tx, p.TenantID+":ward-indent:"+id.String()); err != nil {
			return err
		}
		saved, err := readWardIndent(tx, p.TenantID, outlet, use.hospital, use.department, id, false)
		if err == nil {
			var stored struct{ IntentSha256, ActorSubject string }
			if err := tx.Table("pharmacy_ward_indents").Select("intent_sha256,actor_subject").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&stored).Error; err != nil {
				return err
			}
			if stored.IntentSha256 != hash || stored.ActorSubject != p.Subject {
				return errWardIndentIdentity
			}
			view = saved
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		view, err = h.raiseWardIndent(tx, p, use, id, in, hash)
		return err
	})
	if err != nil {
		wardIndentError(c, err)
		return
	}
	respond.OK(c, view)
}

func (h *catalogueHandlers) raiseWardIndent(tx *gorm.DB, p authn.Principal, use wardIndentUse, id uuid.UUID, in WardIndentInput, hash string) (wardIndentView, error) {
	var stay struct {
		AdmissionID, PatientID      uuid.UUID
		WardCode, RoomCode, BedCode string
	}
	if err := tx.Table("pharmacy_bed_occupancies").Select("admission_id,patient_id,ward_code,room_code,bed_code").
		Where("tenant_id=? AND occupancy_id=? AND hospital_id=? AND started_at IS NOT NULL AND ended_at IS NULL", p.TenantID, in.OccupancyID, use.hospital).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&stay).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return wardIndentView{}, errWardIndentStay
		}
		return wardIndentView{}, err
	}
	basis, err := use.basis()
	if err != nil {
		return wardIndentView{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	var raisedAt time.Time
	if err := tx.Raw(`INSERT INTO pharmacy_ward_indents(tenant_id,outlet_id,id,hospital_id,department_id,kind,occupancy_id,admission_id,patient_id,ward_code,room_code,bed_code,hospital_name,department_name,line_count,status,version,intent_sha256,actor_subject,staff_id,link_id,link_version,generation_id,employment_epoch,authority_basis)
 VALUES(?,?,?,?,?,'patient',?,?,?,?,?,?,?,?,?,'open',1,?,?,?,?,?,?,?,?::jsonb)RETURNING raised_at`,
		p.TenantID, use.outlet, id, use.hospital, use.department, in.OccupancyID, stay.AdmissionID, stay.PatientID, stay.WardCode, stay.RoomCode, stay.BedCode, use.hospitalName, use.departmentName, len(in.Lines), hash, p.Subject,
		use.actor.StaffID, use.actor.LinkID, use.actor.LinkVersion, use.actor.GenerationID, use.actor.EmploymentEpoch, string(basis)).Scan(&raisedAt).Error; err != nil {
		return wardIndentView{}, err
	}
	for i, line := range in.Lines {
		if err := tx.Exec(`INSERT INTO pharmacy_ward_indent_lines(tenant_id,outlet_id,indent_id,line_number,hospital_id,department_id,product_id,requested_units)VALUES(?,?,?,?,?,?,?,?)`,
			p.TenantID, use.outlet, id, i+1, use.hospital, use.department, line.ProductID, line.RequestedUnits).Error; err != nil {
			return wardIndentView{}, err
		}
	}
	if err := tx.Exec(`SET CONSTRAINTS pharmacy_ward_indent_complete IMMEDIATE`).Error; err != nil {
		return wardIndentView{}, err
	}
	if err := h.publishWardIndent(tx, p.TenantID, pharmacycontract.SubjectWardIndentRaised, "WardIndentRaised", pharmacycontract.WardIndentRaisedData{
		IndentID: id.String(), OutletID: use.outlet.String(), HospitalID: use.hospital.String(), DepartmentID: use.department.String(), Kind: "patient", AdmissionID: stay.AdmissionID.String(), RaisedAt: raisedAt.UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return wardIndentView{}, err
	}
	return readWardIndent(tx, p.TenantID, use.outlet, use.hospital, use.department, id, false)
}

func (h *catalogueHandlers) putWardIndentCancellation(c *gin.Context) {
	p, outlet, ok := wardIndentRequest(c, true)
	if !ok {
		return
	}
	id, ok := wardIndentID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var view wardIndentView
	err := h.wardIndentWork(ctx, p, c.Param("hospital"), c.Param("department"), outlet, PermIndentRaise, false, func(tx *gorm.DB, use wardIndentUse) error {
		if err := stockLock(tx, p.TenantID+":ward-indent:"+id.String()); err != nil {
			return err
		}
		current, err := readWardIndent(tx, p.TenantID, outlet, use.hospital, use.department, id, true)
		if err != nil {
			return err
		}
		if current.Status == "cancelled" {
			view = current
			return nil
		}
		var closedAt time.Time
		if err := tx.Raw(`UPDATE pharmacy_ward_indents SET status='cancelled',version=2,closed_by=? WHERE tenant_id=? AND outlet_id=? AND id=? AND status='open' RETURNING closed_at`, p.Subject, p.TenantID, outlet, id).Scan(&closedAt).Error; err != nil {
			return err
		}
		if closedAt.IsZero() {
			return errWardIndentIdentity
		}
		if err := h.publishWardIndent(tx, p.TenantID, pharmacycontract.SubjectWardIndentClosed, "WardIndentClosed", pharmacycontract.WardIndentClosedData{
			IndentID: id.String(), OutletID: outlet.String(), Status: "cancelled", ClosedAt: closedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		view, err = readWardIndent(tx, p.TenantID, outlet, use.hospital, use.department, id, false)
		return err
	})
	if err != nil {
		wardIndentError(c, err)
		return
	}
	respond.OK(c, view)
}

func (h *catalogueHandlers) publishWardIndent(tx *gorm.DB, tenant, subject, kind string, data any) error {
	raw, err := json.Marshal(data)
	if err == nil {
		err = h.bus.Publish(tx, subject, events.Event{Type: kind, Version: 1, TenantID: tenant, Data: raw})
	}
	if err != nil {
		return errors.Join(errWardIndentEvidence, err)
	}
	return nil
}
