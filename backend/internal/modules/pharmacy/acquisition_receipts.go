package pharmacy

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

type acquisitionRow struct {
	AcquisitionReceipt                     `gorm:"embedded"`
	SupplierReceiptID                      *uuid.UUID
	Receipt, Shipment, Summary, Correction json.RawMessage
}
type acquisitionReceipt struct{ AcquisitionReceipt }

func (r acquisitionReceipt) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func projectAcquisitionSupplier(row *acquisitionRow) error {
	if row.SupplierReceiptID == nil {
		if strings.HasPrefix(row.SourceReference, "supplier-receipt:") {
			return f.ErrUnavailable
		}
		return nil
	}
	var receipt f.ReceivingReceipt
	var shipment f.Shipment
	var summary f.ReceivingSummary
	if supplierorder.DecodeObject(row.Receipt, &receipt) != nil || supplierorder.DecodeObject(row.Shipment, &shipment) != nil || supplierorder.DecodeObject(row.Summary, &summary) != nil || f.ValidateShipment(shipment) != nil {
		return f.ErrUnavailable
	}
	if *row.SupplierReceiptID != row.ID || receipt.ID != row.ID || receipt.OwnerTenantID != row.OwnerTenantID || receipt.PharmacyID != row.PharmacyID || receipt.OutletID != row.OutletID || receipt.Input.AcceptedUnits != row.QuantityUnits || receipt.Input.DestinationBatchID == nil || *receipt.Input.DestinationBatchID != row.BatchID || receipt.Input.DestinationProductID == nil || *receipt.Input.DestinationProductID != row.ProductID || receipt.Input.DestinationProductVersion == nil || *receipt.Input.DestinationProductVersion != row.ProductVersion {
		return f.ErrUnavailable
	}
	if shipment.ID != receipt.ShipmentID || shipment.OrderID != receipt.OrderID || shipment.HospitalID != receipt.HospitalID || shipment.BuyerTenantID != row.OwnerTenantID || shipment.BuyerOrganizationID != row.PharmacyID || shipment.BuyerLocationID != row.OutletID || shipment.UnitCode != row.UnitCode || summary.OwnerTenantID != row.OwnerTenantID || summary.ShipmentID != shipment.ID || summary.OrderID != shipment.OrderID || summary.Version < 1 || summary.CountedUnits > shipment.QuantityUnits || summary.AcceptedUnits+summary.RejectedUnits != summary.CountedUnits || summary.ShortUnits < 0 || summary.RejectedUnits < 0 {
		return f.ErrUnavailable
	}
	if summary.Closed && summary.ShortUnits != shipment.QuantityUnits-summary.CountedUnits || !summary.Closed && summary.ShortUnits != 0 {
		return f.ErrUnavailable
	}
	result := &AcquisitionSupplier{ShipmentID: shipment.ID, OrderID: shipment.OrderID, SourceProduct: shipment.Product.Owner, OriginalAcceptedUnits: row.QuantityUnits, EffectiveAcceptedUnits: row.QuantityUnits, ReceiptVersion: 1, SummaryVersion: summary.Version, ReceivingClosed: summary.Closed, DiscrepancyUnits: summary.ShortUnits + summary.RejectedUnits}
	if len(row.Correction) > 0 {
		var correction f.ReceivingCorrection
		if supplierorder.DecodeObject(row.Correction, &correction) != nil || correction.OwnerTenantID != row.OwnerTenantID || correction.ReceiptID != row.ID || correction.OutletID != row.OutletID || correction.ShipmentID != shipment.ID || correction.Version < 2 || correction.Version > 1000 || correction.Input.AcceptedUnits < 0 || correction.Input.AcceptedUnits > correction.Input.CountedUnits {
			return f.ErrUnavailable
		}
		result.EffectiveAcceptedUnits = correction.Input.AcceptedUnits
		result.ReceiptVersion = correction.Version
	}
	row.Supplier = result
	return nil
}

func withAcquisitionScope(tx *gorm.DB, outlet uuid.UUID, action func() error) (err error) {
	var old string
	if err = tx.Raw(`SELECT jsonb_build_object('app.pharmacy_acquisition_read',COALESCE(current_setting('app.pharmacy_acquisition_read',true),''),'app.pharmacy_acquisition_outlet',COALESCE(current_setting('app.pharmacy_acquisition_outlet',true),''))::text`).Scan(&old).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, old).Error)
	}()
	if err = tx.Exec(`SELECT set_config('app.pharmacy_acquisition_read','authorized',true),set_config('app.pharmacy_acquisition_outlet',?,true)`, outlet.String()).Error; err != nil {
		return err
	}
	return action()
}

func (h *catalogueHandlers) listAcquisitionReceipts(c *gin.Context, params pagination.Params) ([]acquisitionReceipt, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	headers := c.Request.Header.Values("X-Helivanta-Expected-Subject")
	tenantHeaders := c.Request.Header.Values("X-Helivanta-Expected-Tenant")
	if len(headers) != 1 || headers[0] != p.Subject || len(tenantHeaders) != 1 || tenantHeaders[0] != p.TenantID {
		respond.Error(c, 409, "session_changed", "The financial account changed. Reload before choosing receipts.")
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, f.ErrInvalid)
			return nil, nil
		}
	}
	result := []acquisitionReceipt{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return withAcquisitionScope(tx, outlet, func() error {
			query := tx.Table("pharmacy_stock_movements m").Select(`m.id,m.tenant_id AS owner_tenant_id,?::uuid AS pharmacy_id,m.outlet_id,m.batch_id,b.product_id,b.product_version,r.snapshot->>'currency' AS currency,r.snapshot->>'unit_of_issue' AS unit_code,m.quantity_units,m.source_reference,m.created_at,l.payable_id,s.id AS supplier_receipt_id,s.body AS receipt,h.shipment,h.summary,c.body AS correction`, currentPharmacy(c.Request.Context())).
				Joins("JOIN pharmacy_stock_batches b ON b.tenant_id=m.tenant_id AND b.outlet_id=m.outlet_id AND b.id=m.batch_id").
				Joins("JOIN pharmacy_catalogue_revisions r ON r.tenant_id=b.tenant_id AND r.outlet_id=b.outlet_id AND r.product_id=b.product_id AND r.version=b.product_version").
				Joins("LEFT JOIN pharmacy_payable_lines l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.receipt_id=m.id").
				Joins("LEFT JOIN pharmacy_supplier_receipts s ON s.tenant_id=m.tenant_id AND s.outlet_id=m.outlet_id AND s.id=m.id").
				Joins("LEFT JOIN pharmacy_supplier_receiving_heads h ON h.tenant_id=s.tenant_id AND h.outlet_id=s.outlet_id AND h.shipment_id=s.shipment_id").
				Joins("LEFT JOIN LATERAL (SELECT body FROM pharmacy_supplier_receiving_corrections correction WHERE correction.tenant_id=s.tenant_id AND correction.outlet_id=s.outlet_id AND correction.receipt_id=s.id ORDER BY version DESC LIMIT 1)c ON true").
				Where("m.tenant_id=? AND m.outlet_id=? AND m.kind='receipt'", p.TenantID, outlet).Order("m.created_at DESC,m.id DESC").Limit(params.Limit + 1)
			if params.After != nil {
				query = query.Where("(m.created_at,m.id)<(?,?)", params.After.CreatedAt, params.After.ID)
			}
			var rows []acquisitionRow
			if err := query.Scan(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				if err := projectAcquisitionSupplier(&rows[i]); err != nil {
					return err
				}
				result = append(result, acquisitionReceipt{rows[i].AcquisitionReceipt})
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'pharmacy_acquisition_receipts_read','pharmacy_outlet',?)`, p.TenantID, p.Subject, outlet).Error
		})
	})
	if err != nil {
		if errors.Is(err, f.ErrUnavailable) {
			supplierstock.Error(c, err)
		} else {
			payableError(c, err)
		}
	}
	return result, nil
}
