package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"reflect"
)

const (
	PermHospitalReceivingUse    authz.Permission = "pharmacy.hospital_receiving.use"
	PermHospitalReceivingReview authz.Permission = "pharmacy.hospital_receiving.review"
)

type supplierReceiving struct {
	db        *tenantdb.DB
	authority f.ReceivingAuthority
	safety    f.ShipmentSafety
}

func supplierReceivingRoutes(r *platform.Router, deps platform.Deps) {
	h := &supplierReceiving{db: deps.DB, authority: deps.ReceivingAuthority, safety: deps.SupplierShipmentSafety}
	base := "/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment"
	r.GET(base+"/receiving", authz.Public, h.current)
	r.PUT(base+"/receipts/:receipt", authz.Public, h.receive)
	r.GET(base+"/receipts/:receipt", authz.Public, h.receipt)
	r.PUT(base+"/closures/:closure", authz.Public, h.close)
	r.PUT(base+"/receipts/:receipt/clearances/:clearance", authz.Public, h.clearance)
	r.PUT(base+"/receipts/:receipt/corrections/:correction", authz.Public, h.correct)
	r.GET(base+"/receipts/:receipt/corrections/:correction", authz.Public, h.correction)
}
func receivingID(c *gin.Context, name string, anyUUID bool) (uuid.UUID, bool) {
	v, e := uuid.Parse(c.Param(name))
	if e != nil || v == uuid.Nil || v.Variant() != uuid.RFC4122 || !anyUUID && v.Version() != 7 || v.String() != c.Param(name) {
		respond.NotFound(c, "supplier receipt")
		return uuid.Nil, false
	}
	return v, true
}
func receivingScope(c *gin.Context) (authn.Principal, f.BuyerScope, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, f.BuyerScope{}, false
	}
	p, tenant, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, f.BuyerScope{}, false
	}
	for k, v := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(k)
		if len(values) != 1 || values[0] != v {
			respond.Error(c, 409, "session_changed", "The workspace or account changed. Refresh before recording goods.")
			return p, f.BuyerScope{}, false
		}
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("receiving commands do not accept query parameters"))
		return p, f.BuyerScope{}, false
	}
	hospital, ok := receivingID(c, "hospital", true)
	if !ok {
		return p, f.BuyerScope{}, false
	}
	dept, ok := receivingID(c, "department", false)
	if !ok {
		return p, f.BuyerScope{}, false
	}
	outlet, ok := receivingID(c, "outlet", false)
	if !ok {
		return p, f.BuyerScope{}, false
	}
	shipment, ok := receivingID(c, "shipment", false)
	if !ok {
		return p, f.BuyerScope{}, false
	}
	return p, f.BuyerScope{TenantID: tenant, HospitalID: hospital, DepartmentID: dept, OutletID: outlet, ShipmentID: shipment}, true
}
func (h *supplierReceiving) with(c *gin.Context, p authn.Principal, s f.BuyerScope, purpose f.ReceivingPurpose, action func(context.Context, *gorm.DB, f.Shipment, supplierorder.Actor) error) error {
	if h.db == nil || h.authority == nil {
		return f.ErrUnavailable
	}
	return h.authority.WithShipment(c.Request.Context(), p, s, purpose, func(ctx context.Context, shipment f.Shipment, a supplierorder.Actor) error {
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
			keys := []string{"app.pharmacy_subject", "app.pharmacy_receiving_access", "app.pharmacy_receiving_outlet", "app.pharmacy_receiving_pharmacy", "app.pharmacy_receiving_hospital", "app.pharmacy_receiving_department", "app.pharmacy_receiving_staff", "app.pharmacy_receiving_purpose"}
			raw, _ := json.Marshal(keys)
			var old string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(raw)).Scan(&old).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, old).Error)
			}()
			values := map[string]string{"app.pharmacy_subject": p.Subject, "app.pharmacy_receiving_access": "authorized", "app.pharmacy_receiving_outlet": s.OutletID.String(), "app.pharmacy_receiving_pharmacy": shipment.BuyerOrganizationID.String(), "app.pharmacy_receiving_hospital": s.HospitalID.String(), "app.pharmacy_receiving_department": s.DepartmentID.String(), "app.pharmacy_receiving_staff": a.StaffID.String(), "app.pharmacy_receiving_purpose": string(purpose)}
			raw, err = json.Marshal(values)
			if err != nil {
				return err
			}
			if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, string(raw)).Error; err != nil {
				return err
			}
			if err = stockLock(tx, p.TenantID+":supplier-receiving:"+s.ShipmentID.String()); err != nil {
				return err
			}
			return action(ctx, tx, shipment, a)
		})
	})
}

type receivingHead struct {
	Version, CountedUnits, AcceptedUnits, RejectedUnits int64
	Closed                                              bool
	Summary                                             json.RawMessage
}

func readReceivingHead(tx *gorm.DB, tenant, shipment uuid.UUID) (receivingHead, error) {
	var head receivingHead
	err := tx.Table("pharmacy_supplier_receiving_heads").Where("tenant_id=? AND shipment_id=?", tenant, shipment).Take(&head).Error
	return head, err
}
func (h *supplierReceiving) current(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	var result *f.ReceivingSummary
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, _ supplierorder.Actor) error {
		head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result = new(f.ReceivingSummary)
		return supplierorder.DecodeObject(head.Summary, result)
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"summary": result, "state": map[bool]string{true: "not_recorded", false: "recorded"}[result == nil]})
}
func (h *supplierReceiving) read(tx *gorm.DB, tenant, shipment, id uuid.UUID, actor supplierorder.Actor, in *f.ReceivingInput) (f.ReceivingReceipt, error) {
	var row struct {
		Body       json.RawMessage
		IntentHash string
	}
	var r f.ReceivingReceipt
	err := tx.Table("pharmacy_supplier_receipts").Where("tenant_id=? AND shipment_id=? AND id=?", tenant, shipment, id).Take(&row).Error
	if err != nil {
		return r, err
	}
	if supplierorder.DecodeObject(row.Body, &r) != nil || r.ID != id || r.ShipmentID != shipment || r.OwnerTenantID != tenant {
		return r, f.ErrUnavailable
	}
	if in != nil && (r.ActorSubject != actor.Subject || r.ActorStaffID != actor.StaffID) {
		return r, f.ErrConflict
	}
	if in != nil {
		hash, e := f.IntentHash(actor, *in)
		if e != nil || hash != row.IntentHash {
			return r, f.ErrConflict
		}
	}
	return r, nil
}
func (h *supplierReceiving) receipt(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	id, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	var result f.ReceivingReceipt
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, a supplierorder.Actor) error {
		var err error
		result, err = h.read(tx, s.TenantID, s.ShipmentID, id, a, nil)
		return err
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierReceiving) receive(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	id, ok := receivingID(c, "receipt", false)
	if !ok {
		return
	}
	var in f.ReceivingInput
	if !decodeSupplierReceiving(c, &in) {
		return
	}
	var result f.ReceivingReceipt
	err := h.with(c, p, s, f.RecordReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, a supplierorder.Actor) error {
		prior, err := h.read(tx, s.TenantID, s.ShipmentID, id, a, &in)
		if err == nil {
			result = prior
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if head.Closed {
			return f.ErrConflict
		}
		var count int64
		if err = tx.Table("pharmacy_supplier_receipts").Where("tenant_id=? AND shipment_id=?", s.TenantID, s.ShipmentID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1000 {
			return f.ErrConflict
		}
		at, err := supplierstock.Clock(tx)
		if err != nil {
			return err
		}
		result, err = f.Receive(id, shipment, in, f.ReceivingTotals{Counted: head.CountedUnits, Accepted: head.AcceptedUnits, Rejected: head.RejectedUnits}, a, at)
		if err != nil {
			return err
		}
		summary := f.ReceivingSummary{ID: id, ShipmentID: s.ShipmentID, OrderID: shipment.OrderID, OwnerTenantID: s.TenantID, Version: head.Version + 1, CountedUnits: result.CumulativeCountedUnits, AcceptedUnits: result.CumulativeAcceptedUnits, RejectedUnits: result.CumulativeRejectedUnits, Reason: in.Reason, ActorSubject: a.Subject, RecordedAt: at}
		if err = saveReceivingHead(tx, s, shipment, summary); err != nil {
			return err
		}
		hash, err := f.IntentHash(a, in)
		if err != nil {
			return err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO pharmacy_supplier_receipts(tenant_id,id,shipment_id,pharmacy_id,outlet_id,destination_batch_id,quarantine_location_id,counted_units,accepted_units,rejected_units,intent_hash,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?)`, s.TenantID, id, s.ShipmentID, shipment.BuyerOrganizationID, s.OutletID, in.DestinationBatchID, in.QuarantineLocationID, in.CountedUnits, in.AcceptedUnits, in.RejectedUnits, hash, string(body), a.Subject, a.StaffID, at).Error; err != nil {
			return err
		}
		if in.AcceptedUnits > 0 {
			if err = h.postQuarantine(tx, p, shipment, result, summary.Version); err != nil {
				return err
			}
		}
		return publishReceivingSummary(tx, shipment, summary)
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierReceiving) postQuarantine(tx *gorm.DB, p authn.Principal, s f.Shipment, r f.ReceivingReceipt, summaryVersion int64) error {
	in := r.Input
	var product catalogueProduct
	if err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", s.BuyerTenantID, s.BuyerLocationID, *in.DestinationProductID).Take(&product).Error; err != nil {
		return err
	}
	if product.Status != "active" || product.Version != *in.DestinationProductVersion || product.UnitOfIssue != s.UnitCode || int64(product.PackSize) != s.PackSize {
		return f.ErrConflict
	}
	var location stockLocation
	if err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", s.BuyerTenantID, s.BuyerLocationID, *in.QuarantineLocationID).Take(&location).Error; err != nil {
		return err
	}
	if location.StockStatus != "quarantine" {
		return f.ErrConflict
	}
	if _, _, err := recordBatch(tx, p.TenantID, s.BuyerLocationID, *in.DestinationBatchID, batchInput{ProductID: *in.DestinationProductID, LotNumber: in.ObservedLotNumber, ExpiresOn: in.ObservedExpiresOn, MRPMinor: *in.MrpMinor}); err != nil {
		return err
	}
	source, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO pharmacy_supplier_batch_sources(tenant_id,outlet_id,batch_id,receipt_id,shipment_id,source,current_summary_version)VALUES(?,?,?,?,?,?::jsonb,?)`, s.BuyerTenantID, s.BuyerLocationID, *in.DestinationBatchID, r.ID, s.ID, string(source), summaryVersion).Error; err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at)VALUES(?,?,?,?,?,'receipt',?,?,?,?,?)`, s.BuyerTenantID, s.BuyerLocationID, r.ID, *in.QuarantineLocationID, *in.DestinationBatchID, in.AcceptedUnits, in.Reason, "supplier-receipt:"+r.ID.String(), r.ActorSubject, r.CreatedAt).Error
}
func saveReceivingHead(tx *gorm.DB, scope f.BuyerScope, s f.Shipment, v f.ReceivingSummary) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	shipment, err := json.Marshal(s)
	if err != nil {
		return err
	}
	result := tx.Exec(`INSERT INTO pharmacy_supplier_receiving_heads(tenant_id,shipment_id,order_id,hospital_id,department_id,pharmacy_id,outlet_id,version,counted_units,accepted_units,rejected_units,closed,shipment,summary,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,?,?)ON CONFLICT(tenant_id,shipment_id)DO UPDATE SET version=EXCLUDED.version,counted_units=EXCLUDED.counted_units,accepted_units=EXCLUDED.accepted_units,rejected_units=EXCLUDED.rejected_units,closed=EXCLUDED.closed,summary=EXCLUDED.summary,updated_at=EXCLUDED.updated_at WHERE pharmacy_supplier_receiving_heads.version=EXCLUDED.version-1`, scope.TenantID, scope.ShipmentID, s.OrderID, scope.HospitalID, scope.DepartmentID, s.BuyerOrganizationID, scope.OutletID, v.Version, v.CountedUnits, v.AcceptedUnits, v.RejectedUnits, v.Closed, string(shipment), string(raw), v.RecordedAt, v.RecordedAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return f.ErrConflict
	}
	return tx.Exec(`UPDATE pharmacy_supplier_batch_sources SET current_summary_version=? WHERE tenant_id=? AND shipment_id=?`, v.Version, scope.TenantID, scope.ShipmentID).Error
}

func (h *supplierReceiving) close(c *gin.Context) {
	p, s, ok := receivingScope(c)
	if !ok {
		return
	}
	id, ok := receivingID(c, "closure", false)
	if !ok {
		return
	}
	var in f.ReceivingClosureInput
	if !decodeSupplierReceiving(c, &in) {
		return
	}
	var result f.ReceivingClosure
	err := h.with(c, p, s, f.RecordReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, a supplierorder.Actor) error {
		var row struct{ Body, Input json.RawMessage }
		err := tx.Table("pharmacy_supplier_receiving_closures").Where("tenant_id=? AND id=? AND shipment_id=?", s.TenantID, id, s.ShipmentID).Take(&row).Error
		if err == nil {
			var original f.ReceivingClosureInput
			if supplierorder.DecodeObject(row.Body, &result) != nil || supplierorder.DecodeObject(row.Input, &original) != nil {
				return f.ErrUnavailable
			}
			if !reflect.DeepEqual(original, in) || result.ActorSubject != a.Subject || result.ActorStaffID != a.StaffID {
				return f.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		head, err := readReceivingHead(tx, s.TenantID, s.ShipmentID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if head.Closed || in.ExpectedSummaryVersion != nil && *in.ExpectedSummaryVersion != head.Version {
			return f.ErrConflict
		}
		at, err := supplierstock.Clock(tx)
		if err != nil {
			return err
		}
		result, err = f.CloseReceiving(id, shipment, f.ReceivingTotals{Counted: head.CountedUnits, Accepted: head.AcceptedUnits, Rejected: head.RejectedUnits}, in, a, at)
		if err != nil {
			return err
		}
		summary := f.ReceivingSummary{ID: id, ShipmentID: s.ShipmentID, OrderID: shipment.OrderID, OwnerTenantID: s.TenantID, Version: head.Version + 1, CountedUnits: result.CountedUnits, AcceptedUnits: result.AcceptedUnits, RejectedUnits: result.RejectedUnits, ShortUnits: result.ShortUnits, Closed: true, Reason: in.Reason, ActorSubject: a.Subject, RecordedAt: at}
		if err = saveReceivingHead(tx, s, shipment, summary); err != nil {
			return err
		}
		body, _ := json.Marshal(result)
		input, _ := json.Marshal(in)
		if err = tx.Exec(`INSERT INTO pharmacy_supplier_receiving_closures(tenant_id,id,shipment_id,outlet_id,summary_version,input,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?::jsonb,?::jsonb,?,?,?)`, s.TenantID, id, s.ShipmentID, s.OutletID, summary.Version, string(input), string(body), a.Subject, a.StaffID, at).Error; err != nil {
			return err
		}
		return publishReceivingSummary(tx, shipment, summary)
	})
	if err != nil {
		supplierstock.Error(c, err)
		return
	}
	respond.OK(c, result)
}
