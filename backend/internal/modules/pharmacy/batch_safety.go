package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type batchSafetyIncident struct {
	ID                              uuid.UUID  `json:"id"`
	TenantID                        uuid.UUID  `json:"-"`
	OutletID                        uuid.UUID  `json:"outlet_id"`
	BatchID                         uuid.UUID  `json:"batch_id"`
	LocationID                      *uuid.UUID `json:"location_id"`
	Kind                            string     `json:"kind"`
	Reason                          string     `json:"reason"`
	OccurredAt                      time.Time  `json:"occurred_at"`
	MeasuredTemperatureMillicelsius *int64     `json:"measured_temperature_millicelsius"`
	ActorSubject                    string     `json:"actor_subject"`
	CreatedAt                       time.Time  `json:"recorded_at" gorm:"autoCreateTime:false;default:clock_timestamp()"`
}

var safetyInstant = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func (batchSafetyIncident) TableName() string                 { return "pharmacy_batch_safety_incidents" }
func (r batchSafetyIncident) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (r batchSafetyIncident) MarshalJSON() ([]byte, error) {
	type fields batchSafetyIncident
	r.OccurredAt = r.OccurredAt.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	return json.Marshal(fields(r))
}

func sameSafetyIncident(a, b batchSafetyIncident) bool {
	return a.BatchID == b.BatchID && a.Kind == b.Kind && a.Reason == b.Reason && a.ActorSubject == b.ActorSubject && a.OccurredAt.Equal(b.OccurredAt) && sameSafetyOptional(a.LocationID, b.LocationID) && sameSafetyOptional(a.MeasuredTemperatureMillicelsius, b.MeasuredTemperatureMillicelsius)
}

func sameSafetyOptional[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (h *catalogueHandlers) putBatchSafetyIncident(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in safetyIncidentInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	occurred, err := time.Parse(time.RFC3339Nano, in.OccurredAt)
	validID := func(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
	if err != nil || !safetyInstant.MatchString(in.OccurredAt) || occurred.UTC().Year() < 1 || utf8.RuneCountInString(in.Reason) < 10 || !validID(in.BatchID) || (in.LocationID != nil && !validID(*in.LocationID)) || (in.Kind == "temperature_excursion") != (in.MeasuredTemperatureMillicelsius != nil) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid batch safety incident")
		return
	}
	desired := batchSafetyIncident{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, BatchID: in.BatchID, LocationID: in.LocationID, Kind: in.Kind, Reason: in.Reason, OccurredAt: occurred.UTC(), MeasuredTemperatureMillicelsius: in.MeasuredTemperatureMillicelsius, ActorSubject: p.Subject}
	var result batchSafetyIncident
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":safety:"+id.String()); err != nil {
			return err
		}
		err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		if err == nil {
			if !sameSafetyIncident(result, desired) {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, in.BatchID); err != nil {
			return err
		}
		if in.LocationID != nil {
			if err := stockReference(tx, "pharmacy_stock_locations", p.TenantID, outlet, *in.LocationID); err != nil {
				return err
			}
		}
		if err := validateStockCutoff(tx, occurred); err != nil {
			return err
		}
		result = desired
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.BatchSafetyRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), BatchID: in.BatchID.String(), IncidentID: id.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectBatchSafetyRecorded, events.Event{Type: "BatchSafetyRecorded", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) listBatchSafetyIncidents(c *gin.Context, params pagination.Params) ([]batchSafetyIncident, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	// Without a batch, the list is the outlet's recent concerns (#576 D4),
	// which is what the overview counts. Any other unscoped request is
	// refused rather than listing the whole journal.
	if c.Query("batch_id") == "" {
		if c.Query("recorded") != "30d" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "give batch_id, or recorded=30d for the outlet's recent concerns")
			return nil, nil
		}
		var rows []batchSafetyIncident
		err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
			var o pharmacyOutlet
			if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&o).Error; err != nil {
				return err
			}
			today, err := outletToday(o.Timezone)
			if err != nil {
				return err
			}
			q := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
			return recentSafetyScope(today, o.Timezone)(q).Find(&rows).Error
		})
		return rows, err
	}
	batch, ok := stockFilter(c, "batch_id", true)
	if !ok {
		return nil, nil
	}
	var rows []batchSafetyIncident
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, batch); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND outlet_id=? AND batch_id=?", p.TenantID, outlet, batch).Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

// recentSafetyScope keeps concerns recorded in the last 30 days of the
// outlet's own calendar, today included (#576). Shared by the outlet-wide
// list and the overview count so the two cannot disagree.
func recentSafetyScope(today, timezone string) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		return tx.Where("(created_at AT TIME ZONE ?)::date >= ?::date - 29", timezone, today)
	}
}
