package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type catalogueHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

type catalogueProduct struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"-"`
	OutletID         uuid.UUID `json:"outlet_id"`
	ProductFields    `gorm:"embedded"`
	RegulatoryStatus string    `json:"regulatory_status"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// Tax is the classification in force on the outlet's date (#1283).
	Tax *productTax `json:"tax" gorm:"-"`
}

func (catalogueProduct) TableName() string                 { return "pharmacy_catalogue_products" }
func (p catalogueProduct) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

type catalogueRevision struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"-"`
	OutletID     uuid.UUID       `json:"outlet_id"`
	ProductID    uuid.UUID       `json:"product_id"`
	Version      int64           `json:"version"`
	Snapshot     json.RawMessage `json:"snapshot" gorm:"type:jsonb"`
	ActorSubject string          `json:"actor_subject"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (catalogueRevision) TableName() string                 { return "pharmacy_catalogue_revisions" }
func (r catalogueRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const productColumns = "id,tenant_id,outlet_id,sku,brand_name,generic_name,dosage_form,route,manufacturer,marketing_company,ingredients,pack_size,unit_of_issue,currency,mrp_minor,status,regulatory_status,version,created_at,updated_at"

var skuPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,39}$`)
var strengthPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(\.[0-9]{1,6})?$`)
var errPharmacyAccessUnavailable = errors.New("pharmacy access unavailable")
var errCatalogueVersion = errors.New("catalogue version conflict")
var errCatalogueWithdrawn = errors.New("withdrawn product is immutable")

func catalogueRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB, bus: deps.Bus}
	s := g.Group("/pharmacies/:pharmacy/outlets/:outlet")
	s.GET("/access", PermCatalogueRead, h.getAccess)
	s.GET("/overview", PermCatalogueRead, h.getOverview)
	s.PUT("/access/:subject", PermAccessManage, h.putAccess)
	s.DELETE("/access/:subject", PermAccessManage, h.deleteAccess)
	s.PUT("/catalogue/products/:id", PermCatalogueWrite, h.putProduct)
	s.GET("/catalogue/products/:id", PermCatalogueRead, h.getProduct)
	platform.ListRoute(s, "/catalogue/products", PermCatalogueRead, h.listProducts)
	platform.GuardedCountedListRoute[catalogueProduct](s, h.db, "/catalogue/products/counted", PermCatalogueRead, tenantdb.Keyset{Column: "created_at"}, countedProducts)
	platform.ListRoute(s, "/catalogue/products/:id/revisions", PermCatalogueRead, h.listRevisions)
	taxClassificationRoutes(s, h)
}

func decodeCatalogue(c *gin.Context, in any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(in); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue request")
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "expected one JSON object")
		return false
	}
	if err := binding.Validator.ValidateStruct(in); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue fields")
		return false
	}
	return true
}

func catalogueScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	pharmacy, err := uuid.Parse(c.Param("pharmacy"))
	if err != nil || pharmacy == uuid.Nil {
		respond.NotFound(c, "pharmacy")
		return p, uuid.Nil, false
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), pharmacyScopeKey{}, pharmacy))
	outlet, err := uuid.Parse(c.Param("outlet"))
	if err != nil || outlet == uuid.Nil {
		respond.NotFound(c, "outlet")
		return p, uuid.Nil, false
	}
	return p, outlet, true
}

func catalogueID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "product")
		return uuid.Nil, false
	}
	return id, true
}

func (h *catalogueHandlers) transaction(ctx context.Context, p authn.Principal, outlet uuid.UUID, writing bool, fn func(*gorm.DB) error) error {
	pharmacy := currentPharmacy(ctx)
	if pharmacy == uuid.Nil {
		return gorm.ErrRecordNotFound
	}
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		level, err := outletAccess(tx, p, outlet, pharmacy, true)
		if err != nil {
			return err
		}
		if writing && level != "editor" {
			return gorm.ErrRecordNotFound
		}
		return fn(tx)
	})
}

// outletAccess sets the pharmacy session settings and checks the caller's
// outlet grant: the one check every outlet read and write runs. lock takes
// FOR SHARE so a write cannot race a revoke; a read-only snapshot (#576)
// refuses the lock and needs none, since the snapshot fixes what it sees.
func outletAccess(tx *gorm.DB, p authn.Principal, outlet, pharmacy uuid.UUID, lock bool) (string, error) {
	if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, p.Subject, outlet.String(), pharmacy.String()).Error; err != nil {
		return "", errPharmacyAccessUnavailable
	}
	query := `SELECT a.level FROM pharmacy_outlet_access a JOIN pharmacy_outlets o ON o.tenant_id=a.tenant_id AND o.id=a.outlet_id WHERE a.tenant_id=? AND a.outlet_id=? AND a.subject=? AND o.pharmacy_id=?`
	if lock {
		query += ` FOR SHARE OF a,o`
	}
	var grant struct{ Level string }
	if err := tx.Raw(query, p.TenantID, outlet, p.Subject, pharmacy).Scan(&grant).Error; err != nil {
		return "", errPharmacyAccessUnavailable
	}
	// Missing or revoked scope denies access, including for tenant
	// administrators: fail closed to the cross-tenant 404.
	if grant.Level == "" {
		return "", gorm.ErrRecordNotFound
	}
	return grant.Level, nil
}

// outletReadGuard is a counted outlet list's guard (#576 D1): the access
// check, then scope builds the filter inside the same snapshot, so any
// outlet date it reads is the one the count and the rows use.
func outletReadGuard(c *gin.Context, p authn.Principal, outlet uuid.UUID, scope func(tx *gorm.DB) (platform.Scope, error)) func(tx *gorm.DB) (platform.Scope, error) {
	pharmacy := currentPharmacy(c.Request.Context())
	return func(tx *gorm.DB) (platform.Scope, error) {
		if pharmacy == uuid.Nil {
			return nil, gorm.ErrRecordNotFound
		}
		if _, err := outletAccess(tx, p, outlet, pharmacy, false); err != nil {
			return nil, err
		}
		return scope(tx)
	}
}

func catalogueError(c *gin.Context, err error) {
	if errors.Is(err, errPharmacyAccessUnavailable) {
		respond.Error(c, http.StatusServiceUnavailable, "authorization_unavailable", "pharmacy access is temporarily unavailable")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "pharmacy resource")
		return
	}
	if errors.Is(err, errCatalogueVersion) {
		respond.Conflict(c, "product changed; reload before saving")
		return
	}
	if errors.Is(err, errCatalogueWithdrawn) {
		respond.Conflict(c, "withdrawn products cannot be changed")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.ConstraintName == "pharmacy_stock_product_identity" {
			respond.Conflict(c, "stocked product identity requires a new product")
			return
		}
		switch pg.Code {
		case "23505":
			respond.Conflict(c, "SKU already exists in this outlet")
			return
		case "23503", "42501":
			respond.NotFound(c, "pharmacy resource")
			return
		case "23514", "23502":
			respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue fields")
			return
		}
	}
	// SQL errors may include private product data; never emit the raw database error.
	respond.InternalErr(c, errors.New("pharmacy database operation failed"), "could not process pharmacy request")
}

func (h *catalogueHandlers) putAccess(c *gin.Context)    { h.changeAccess(c, false) }
func (h *catalogueHandlers) deleteAccess(c *gin.Context) { h.changeAccess(c, true) }
func (h *catalogueHandlers) changeAccess(c *gin.Context, revoke bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	if strings.TrimSpace(subject) != subject || len(subject) == 0 || len(subject) > 200 {
		respond.NotFound(c, "subject")
		return
	}
	var in struct {
		Level string `json:"level" binding:"required,oneof=reader editor"`
	}
	if !revoke && !decodeCatalogue(c, &in) {
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		catalogueError(c, err)
		return
	}
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("pharmacy_outlets").Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, currentPharmacy(c.Request.Context()), outlet).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		if revoke {
			return tx.Exec(`DELETE FROM pharmacy_outlet_access WHERE tenant_id=? AND outlet_id=? AND subject=?`, p.TenantID, outlet, subject).Error
		}
		return tx.Exec(`INSERT INTO pharmacy_outlet_access (id,tenant_id,outlet_id,subject,level) VALUES (?,?,?,?,?)
  ON CONFLICT (tenant_id,outlet_id,subject) DO UPDATE SET level=EXCLUDED.level,updated_at=clock_timestamp()
  WHERE pharmacy_outlet_access.level IS DISTINCT FROM EXCLUDED.level`, id, p.TenantID, outlet, subject, in.Level).Error
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, gin.H{"outlet_id": outlet, "subject": subject, "level": in.Level, "revoked": revoke})
}

func (f *ProductFields) normalize() bool {
	f.SKU = strings.ToUpper(strings.TrimSpace(f.SKU))
	for _, field := range []*string{&f.BrandName, &f.GenericName, &f.DosageForm, &f.Route, &f.Manufacturer, &f.UnitOfIssue} {
		*field = strings.TrimSpace(*field)
		if *field == "" {
			return false
		}
	}
	f.MarketingCompany = strings.TrimSpace(f.MarketingCompany)
	if !skuPattern.MatchString(f.SKU) || !countryprofile.SupportedCurrency(f.Currency) {
		return false
	}
	seen := map[string]bool{}
	for i := range f.Ingredients {
		a := &f.Ingredients[i]
		a.Name = strings.TrimSpace(a.Name)
		a.Unit = strings.TrimSpace(a.Unit)
		key := strings.ToLower(a.Name)
		if a.Name == "" || a.Unit == "" || !strengthPattern.MatchString(a.Strength) || strings.Trim(a.Strength, "0.") == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func (h *catalogueHandlers) putProduct(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in productInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !in.normalize() {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue fields")
		return
	}
	var result catalogueProduct
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+outlet.String()+":"+id.String()).Error; err != nil {
			return err
		}
		err := tx.Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists && reflect.DeepEqual(result.ProductFields, in.ProductFields) {
			return attachProductTax(tx, p.TenantID, outlet, &result)
		}
		if (!exists && *in.ExpectedVersion != 0) || (exists && *in.ExpectedVersion != result.Version) {
			return errCatalogueVersion
		}
		if exists && result.Status == "withdrawn" {
			return errCatalogueWithdrawn
		}
		if !exists {
			result = catalogueProduct{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, RegulatoryStatus: "pending_review"}
		}
		result.ProductFields = in.ProductFields
		result.Version++
		if exists {
			if err := tx.Model(&catalogueProduct{}).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).
				Select("sku", "brand_name", "generic_name", "dosage_form", "route", "manufacturer", "marketing_company", "ingredients", "pack_size", "unit_of_issue", "currency", "mrp_minor", "status", "version", "updated_at").
				Updates(&result).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := tx.Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error; err != nil {
			return err
		}
		// A revision records identity only; tax has its own effective-dated
		// history. The outer nil Tax shadows the embedded one and is omitted.
		snapshot, err := json.Marshal(struct {
			catalogueProduct
			Tax *productTax `json:"tax,omitempty"`
		}{catalogueProduct: result})
		if err != nil {
			return err
		}
		revisionID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Create(&catalogueRevision{ID: revisionID, TenantID: result.TenantID, OutletID: outlet, ProductID: id, Version: result.Version, Snapshot: snapshot, ActorSubject: p.Subject}).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.CatalogueUpdatedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), ProductID: id.String(), Version: result.Version, Status: result.Status})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, pharmacycontract.SubjectCatalogueUpdated, events.Event{Type: "MedicineCatalogueUpdated", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return attachProductTax(tx, p.TenantID, outlet, &result)
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getProduct(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var row catalogueProduct
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&row).Error; err != nil {
			return err
		}
		return attachProductTax(tx, p.TenantID, outlet, &row)
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *catalogueHandlers) listProducts(c *gin.Context, params pagination.Params) ([]catalogueProduct, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	status := c.Query("status")
	taxFilter := c.Query("tax")
	if len(q) > 200 || (status != "" && status != "active" && status != "discontinued" && status != "withdrawn") || (taxFilter != "" && taxFilter != "unclassified") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue filters")
		return nil, nil
	}
	var rows []catalogueProduct
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		query := tenantdb.ApplyKeyset(tx, params).Select(productColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
		query = productFilters(status, q)(query)
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		if taxFilter == "unclassified" {
			query = unclassifiedScope(today)(query)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		return attachTax(tx, p.TenantID, outlet, rows)
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}

// productFilters is the catalogue's status and search filter, shared by the
// list and its counted form. The search is a prefix of the brand, generic
// name or SKU, with LIKE wildcards in q taken literally.
func productFilters(status, q string) func(*gorm.DB) *gorm.DB {
	return func(query *gorm.DB) *gorm.DB {
		if status != "" {
			query = query.Where("status=?", status)
		}
		if q != "" {
			prefix := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
			query = query.Where("(lower(brand_name) LIKE ? OR lower(generic_name) LIKE ? OR lower(sku) LIKE ?)", prefix, prefix, prefix)
		}
		return query
	}
}

// errNoTaxScheme refuses the unclassified filter where the outlet's country
// has no tax scheme: every product would match, a gap nobody can close.
var errNoTaxScheme = errors.New("outlet has no tax scheme")

// countedProducts is the catalogue with a total (#576 slice 3): the same
// filters and validation as listProducts.
func countedProducts(c *gin.Context) (platform.CountedQuery[catalogueProduct], bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return platform.CountedQuery[catalogueProduct]{}, false
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	status := c.Query("status")
	taxFilter := c.Query("tax")
	if len(q) > 200 || (status != "" && status != "active" && status != "discontinued" && status != "withdrawn") || (taxFilter != "" && taxFilter != "unclassified") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid catalogue filters")
		return platform.CountedQuery[catalogueProduct]{}, false
	}
	// The outlet's date, read once in the guard: the filter and each row's
	// tax badge use the same day, even across midnight.
	var today string
	return platform.CountedQuery[catalogueProduct]{
		Guard: outletReadGuard(c, p, outlet, func(tx *gorm.DB) (platform.Scope, error) {
			var o pharmacyOutlet
			if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&o).Error; err != nil {
				return nil, err
			}
			if _, ok := countryprofile.Tax(o.CountryCode); !ok && taxFilter == "unclassified" {
				return nil, errNoTaxScheme
			}
			var err error
			if today, err = outletToday(o.Timezone); err != nil {
				return nil, err
			}
			return func(query *gorm.DB) *gorm.DB {
				query = query.Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
				query = productFilters(status, q)(query)
				if taxFilter == "unclassified" {
					query = unclassifiedScope(today)(query)
				}
				return query
			}, nil
		}),
		Rows: func(query *gorm.DB) *gorm.DB { return query.Select(productColumns) },
		After: func(tx *gorm.DB, rows []catalogueProduct) error {
			return attachTaxOn(tx, p.TenantID, outlet, today, rows)
		},
		Fail: func(c *gin.Context, err error) {
			if errors.Is(err, errNoTaxScheme) {
				respond.Error(c, http.StatusBadRequest, "invalid_request", "this outlet has no tax scheme to classify against")
				return
			}
			catalogueError(c, err)
		},
	}, true
}

func (h *catalogueHandlers) listRevisions(c *gin.Context, params pagination.Params) ([]catalogueRevision, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := catalogueID(c)
	if !ok {
		return nil, nil
	}
	var rows []catalogueRevision
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var product catalogueProduct
		if err := tx.Select("id").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&product).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select("id,outlet_id,product_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND outlet_id=? AND product_id=?", p.TenantID, outlet, id).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) getAccess(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var grant struct {
		Level string `json:"level"`
	}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT level FROM pharmacy_outlet_access WHERE tenant_id=? AND outlet_id=? AND subject=?`, p.TenantID, outlet, p.Subject).Scan(&grant).Error
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, grant)
}

type pharmacyScopeKey struct{}

func currentPharmacy(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(pharmacyScopeKey{}).(uuid.UUID)
	return id
}

// unclassifiedScope keeps sellable products with no tax classification in
// force on the outlet's date. Withdrawn products are never sold, so they are
// never a gap. Shared by the catalogue filter and the overview (#576).
func unclassifiedScope(today string) func(*gorm.DB) *gorm.DB {
	return func(q *gorm.DB) *gorm.DB {
		return q.Where(`status IN ('active','discontinued') AND NOT EXISTS (SELECT 1 FROM pharmacy_product_tax_classifications t
 WHERE t.tenant_id=pharmacy_catalogue_products.tenant_id AND t.outlet_id=pharmacy_catalogue_products.outlet_id
 AND t.product_id=pharmacy_catalogue_products.id AND t.effective_from <= ?::date)`, today)
	}
}
