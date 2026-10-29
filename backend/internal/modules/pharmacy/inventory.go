package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type stockLocation struct {
	StorageAddress StorageAddressFields `json:"storage_address" gorm:"embedded"`
	ID             uuid.UUID            `json:"id"`
	TenantID       uuid.UUID            `json:"-"`
	OutletID       uuid.UUID            `json:"outlet_id"`
	Code           string               `json:"code"`
	Name           string               `json:"name"`
	StockStatus    string               `json:"stock_status"`
	CreatedAt      time.Time            `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (stockLocation) TableName() string                 { return "pharmacy_stock_locations" }
func (r stockLocation) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type stockBatch struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"-"`
	OutletID       uuid.UUID  `json:"outlet_id"`
	BatchFields    batchInput `gorm:"embedded" json:"-"`
	ProductVersion int64      `json:"product_version"`
	CreatedAt      time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (stockBatch) TableName() string                 { return "pharmacy_stock_batches" }
func (r stockBatch) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (r stockBatch) MarshalJSON() ([]byte, error) {
	type fields stockBatch
	return json.Marshal(struct {
		fields
		batchInput
	}{fields: fields(r), batchInput: r.BatchFields})
}

type stockMovement struct {
	ID             uuid.UUID     `json:"id"`
	TenantID       uuid.UUID     `json:"-"`
	OutletID       uuid.UUID     `json:"outlet_id"`
	MovementFields movementInput `gorm:"embedded" json:"-"`
	ActorSubject   string        `json:"actor_subject"`
	CreatedAt      time.Time     `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (stockMovement) TableName() string                 { return "pharmacy_stock_movements" }
func (r stockMovement) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (r stockMovement) MarshalJSON() ([]byte, error) {
	type fields stockMovement
	return json.Marshal(struct {
		fields
		movementInput
	}{fields: fields(r), movementInput: r.MovementFields})
}

const batchColumns = "id,tenant_id,outlet_id,product_id,product_version,lot_number,to_char(expires_on,'YYYY-MM-DD') AS expires_on,to_char(manufactured_on,'YYYY-MM-DD') AS manufactured_on,mrp_minor,purchase_cost_minor,created_at"
const locationColumns = "id,tenant_id,outlet_id,code,name,zone_code,aisle_code,rack_code,shelf_code,stock_status,created_at"
const movementColumns = "id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at"

var errStockIdentity = errors.New("stock identity is immutable")

func inventoryRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB, bus: deps.Bus}
	stockOverviewRoutes(g, h)
	s := g.Group("/pharmacies/:pharmacy/outlets/:outlet/stock")
	s.PUT("/locations/:id", PermStockReceive, h.putLocation)
	s.PUT("/batches/:id", PermStockReceive, h.putBatch)
	s.GET("/batches/:id", PermStockRead, h.getBatch)
	s.PUT("/movements/:id", PermStockReceive, h.putReceipt)
	s.PUT("/corrections/:id", PermStockCorrect, h.putCorrection)
	s.PUT("/releases/:id", PermStockRelease, h.putRelease)
	s.PUT("/safety-incidents/:id", PermStockQuarantine, h.putBatchSafetyIncident)
	platform.ListRoute(s, "/safety-incidents", PermStockRead, h.listBatchSafetyIncidents)
	platform.GuardedCountedListRoute[batchSafetyIncident](s, h.db, "/safety-incidents/recent", PermStockRead, tenantdb.Keyset{Column: "created_at"}, countedRecentSafety)
	s.GET("/balance", PermStockRead, h.getBalance)
	s.GET("/reconciliation", PermStockRead, h.getStockReconciliation)
	s.GET("/positions.csv", PermStockRead, h.exportStockPositions)
	platform.ListRoute(s, "/positions", PermStockRead, h.listStockPositions)
	platform.ListRoute(s, "/locations", PermStockRead, h.listLocations)
	platform.ListRoute(s, "/batches", PermStockRead, h.listBatches)
	platform.ListRoute(s, "/movements", PermStockRead, h.listMovements)
}

func stockError(c *gin.Context, err error) {
	if errors.Is(err, errStockPeriod) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "stock period cannot end in the future")
		return
	}
	if errors.Is(err, errStockIdentity) {
		respond.Conflict(c, "stock identity or movement already exists with different details")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.ConstraintName == "pharmacy_stock_quantity_bounds" {
			respond.Conflict(c, "stock correction exceeds the permitted balance")
			return
		}
		if pg.ConstraintName == "pharmacy_stock_quarantine_only" || strings.Contains(pg.Message, "received and corrected in quarantine only") {
			respond.Conflict(c, "stock is received and corrected in quarantine only; release it to sell it")
			return
		}
		if pg.ConstraintName == "pharmacy_stock_product_identity" {
			respond.Conflict(c, "stocked product identity requires a new product")
			return
		}
		if pg.Code == "23505" {
			respond.Conflict(c, "stock code, lot or source reference already exists")
			return
		}
	}
	catalogueError(c, err)
}

func stockLock(tx *gorm.DB, key string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, key).Error
}

func (h *catalogueHandlers) putLocation(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in locationInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	if !skuPattern.MatchString(in.Code) || in.Name == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid stock location")
		return
	}
	address := StorageAddressFields{}
	if in.StorageAddress != nil {
		address = *in.StorageAddress
	}
	for _, code := range []*string{&address.ZoneCode, &address.AisleCode, &address.RackCode, &address.ShelfCode} {
		*code = strings.ToUpper(strings.TrimSpace(*code))
		if len(*code) > 32 || (*code != "" && !skuPattern.MatchString(*code)) {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid storage address code")
			return
		}
	}
	// A location's status is fixed when it is created; quarantine unless
	// the caller asks for a sellable one (#1386 A3).
	status := "quarantine"
	if in.StockStatus != nil {
		status = *in.StockStatus
	}
	var result stockLocation
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":"+id.String()); err != nil {
			return err
		}
		err := tx.Select(locationColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		if err == nil {
			if result.Code != in.Code || result.Name != in.Name || result.StorageAddress != address || result.StockStatus != status {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = stockLocation{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, Code: in.Code, Name: in.Name, StockStatus: status, StorageAddress: address}
		return tx.Create(&result).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

func validBatch(in *batchInput) bool {
	in.LotNumber = strings.ToUpper(strings.TrimSpace(in.LotNumber))
	expiry, err := time.Parse(time.DateOnly, in.ExpiresOn)
	if err != nil || in.ProductID.Version() != 7 || in.ProductID.Variant() != uuid.RFC4122 || in.LotNumber == "" {
		return false
	}
	if in.ManufacturedOn != nil {
		made, err := time.Parse(time.DateOnly, *in.ManufacturedOn)
		if err != nil || made.After(expiry) {
			return false
		}
	}
	return true
}

func sameBatch(a, b batchInput) bool {
	madeA, madeB := "", ""
	if a.ManufacturedOn != nil {
		madeA = *a.ManufacturedOn
	}
	if b.ManufacturedOn != nil {
		madeB = *b.ManufacturedOn
	}
	costA, costB := int64(-1), int64(-1)
	if a.PurchaseCostMinor != nil {
		costA = *a.PurchaseCostMinor
	}
	if b.PurchaseCostMinor != nil {
		costB = *b.PurchaseCostMinor
	}
	return a.ProductID == b.ProductID && a.LotNumber == b.LotNumber && a.ExpiresOn == b.ExpiresOn && madeA == madeB &&
		a.MRPMinor == b.MRPMinor && costA == costB
}

func (h *catalogueHandlers) putBatch(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in batchInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !validBatch(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid batch identity or full expiry date")
		return
	}
	var result stockBatch
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":"+id.String()); err != nil {
			return err
		}
		err := tx.Select(batchColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		if err == nil {
			if !sameBatch(result.BatchFields, in) {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var product catalogueProduct
		if err := tx.Raw(`SELECT id,version,status FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=? FOR SHARE`, p.TenantID, outlet, in.ProductID).Scan(&product).Error; err != nil {
			return err
		}
		if product.ID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		if product.Status != "active" {
			return errStockIdentity
		}
		result = stockBatch{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, BatchFields: in, ProductVersion: product.Version}
		return tx.Create(&result).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) putReceipt(c *gin.Context)    { h.putMovement(c, "receipt") }
func (h *catalogueHandlers) putCorrection(c *gin.Context) { h.putMovement(c, "correction") }
func (h *catalogueHandlers) putMovement(c *gin.Context, kind string) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in movementInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.SourceReference = strings.TrimSpace(in.SourceReference)
	if in.Kind != kind || in.QuantityUnits == 0 || (kind == "receipt" && in.QuantityUnits < 0) || in.Reason == "" || in.SourceReference == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid stock movement")
		return
	}
	var result stockMovement
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":"+id.String()); err != nil {
			return err
		}
		err := tx.Select(movementColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		if err == nil {
			if result.MovementFields != in {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = stockMovement{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, MovementFields: in, ActorSubject: p.Subject}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.StockMovementRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), MovementID: id.String(), LocationID: in.LocationID.String(), BatchID: in.BatchID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectStockMovementRecorded, events.Event{Type: "StockMovementRecorded", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

func stockFilter(c *gin.Context, key string, required bool) (uuid.UUID, bool) {
	raw := c.Query(key)
	if raw == "" && !required {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid stock filter")
		return uuid.Nil, false
	}
	return id, true
}

func (h *catalogueHandlers) listLocations(c *gin.Context, params pagination.Params) ([]stockLocation, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []stockLocation
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(locationColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

// getBatch reads one batch of this outlet, for the safety pane's reload and
// links (#1303 E5). Another outlet's or tenant's batch is not found.
func (h *catalogueHandlers) getBatch(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var row stockBatch
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tx.Select(batchColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&row).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *catalogueHandlers) listBatches(c *gin.Context, params pagination.Params) ([]stockBatch, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	product, ok := stockFilter(c, "product_id", false)
	if !ok {
		return nil, nil
	}
	var rows []stockBatch
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx, params).Select(batchColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
		if product != uuid.Nil {
			q = q.Where("product_id=?", product)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) listMovements(c *gin.Context, params pagination.Params) ([]stockMovement, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	location, ok := stockFilter(c, "location_id", false)
	if !ok {
		return nil, nil
	}
	batch, ok := stockFilter(c, "batch_id", false)
	if !ok {
		return nil, nil
	}
	window, ok := parseStockWindow(c, false)
	if !ok {
		return nil, nil
	}
	var rows []stockMovement
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := stockReference(tx, "pharmacy_stock_locations", p.TenantID, outlet, location); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, batch); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Select(movementColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
		if location != uuid.Nil {
			q = q.Where("location_id=?", location)
		}
		if batch != uuid.Nil {
			q = q.Where("batch_id=?", batch)
		}
		if c.Query("as_of") != "" {
			if err := validateStockCutoff(tx, window.AsOf); err != nil {
				return err
			}
			q = q.Where("created_at<=?", window.AsOf)
		}
		if c.Query("from") != "" {
			q = q.Where("created_at>=?", window.From)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) getBalance(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	location, ok := stockFilter(c, "location_id", true)
	if !ok {
		return
	}
	batch, ok := stockFilter(c, "batch_id", true)
	if !ok {
		return
	}
	var total int64
	var status []string
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT l.stock_status FROM pharmacy_stock_locations l CROSS JOIN pharmacy_stock_batches b
   WHERE l.tenant_id=? AND l.outlet_id=? AND l.id=? AND b.tenant_id=l.tenant_id AND b.outlet_id=l.outlet_id AND b.id=?`, p.TenantID, outlet, location, batch).Scan(&status).Error; err != nil {
			return err
		}
		if len(status) != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Raw(`SELECT COALESCE(sum(quantity_units),0) FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=? AND location_id=? AND batch_id=?`, p.TenantID, outlet, location, batch).Scan(&total).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	// Nothing is advertised as available to dispense: quarantine never is,
	// and sellable stock's availability (expiry, recall, reservations) is the
	// sale slice's to compute (#1386 B2), not a raw balance's.
	respond.OK(c, gin.H{"location_id": location, "batch_id": batch, "on_hand_units": total, "dispensable_units": 0, "stock_status": status[0]})
}
