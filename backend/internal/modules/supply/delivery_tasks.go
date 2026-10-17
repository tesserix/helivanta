package supply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"time"
)

type deliveryTask struct {
	ID                   uuid.UUID `json:"id"`
	TenantID             uuid.UUID `json:"-"`
	PartyID              uuid.UUID `json:"party_id"`
	PremiseID            uuid.UUID `json:"premise_id"`
	SourceKind           string    `json:"source_kind"`
	AssignmentGeneration int64     `json:"assignment_generation"`
	Version              int64     `json:"version"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	AllowedActions       []string  `json:"allowed_actions" gorm:"-"`
	DeliveryTaskFields
}

func (deliveryTask) TableName() string                 { return "supply_delivery_tasks" }
func (r deliveryTask) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const deliveryColumns = `id,tenant_id,party_id,premise_id,code,source_kind,source_reference,destination_business_name,destination_line1,destination_line2,destination_city,destination_region,destination_postal_code,country_code,business_contact_name,business_contact_phone,to_char(window_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS window_start,to_char(window_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS window_end,package_count,handling_notes,driver_subject,COALESCE(to_char(assignment_expires_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'') AS assignment_expires_at,assignment_generation,status,version,created_at,updated_at`
const driverColumns = `id,tenant_id,party_id,premise_id,code,destination_business_name,destination_line1,destination_line2,destination_city,destination_region,destination_postal_code,country_code,business_contact_name,business_contact_phone,to_char(window_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS window_start,to_char(window_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS window_end,package_count,handling_notes,COALESCE(to_char(assignment_expires_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'') AS assignment_expires_at,assignment_generation,status,version,created_at,updated_at`
const deliveryTimeFormat = "2006-01-02T15:04:05.000000Z"

var deliveryControllers = []string{"administrator", "dispatcher"}
var deliveryReaders = []string{"administrator", "dispatcher", "auditor", "regulator"}

type driverDeliveryTask struct {
	ID                      uuid.UUID `json:"id"`
	PartyID                 uuid.UUID `json:"party_id"`
	PremiseID               uuid.UUID `json:"premise_id"`
	Code                    string    `json:"code"`
	DestinationBusinessName string    `json:"destination_business_name"`
	DestinationLine1        string    `json:"destination_line1"`
	DestinationLine2        string    `json:"destination_line2"`
	DestinationCity         string    `json:"destination_city"`
	DestinationRegion       string    `json:"destination_region"`
	DestinationPostalCode   string    `json:"destination_postal_code"`
	CountryCode             string    `json:"country_code"`
	BusinessContactName     string    `json:"business_contact_name"`
	BusinessContactPhone    string    `json:"business_contact_phone"`
	WindowStart             string    `json:"window_start"`
	WindowEnd               string    `json:"window_end"`
	PackageCount            int       `json:"package_count"`
	HandlingNotes           string    `json:"handling_notes"`
	AssignmentExpiresAt     string    `json:"assignment_expires_at"`
	AssignmentGeneration    int64     `json:"assignment_generation"`
	Status                  string    `json:"status"`
	Version                 int64     `json:"version"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
	AllowedActions          []string  `json:"allowed_actions"`
}

func (r driverDeliveryTask) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func projectDriver(t deliveryTask) driverDeliveryTask {
	actions := []string{}
	switch t.Status {
	case "assigned":
		actions = []string{"start", "exception"}
	case "in_transit":
		actions = []string{"deliver", "exception"}
	}
	return driverDeliveryTask{ID: t.ID, PartyID: t.PartyID, PremiseID: t.PremiseID, Code: t.Code, DestinationBusinessName: t.DestinationBusinessName, DestinationLine1: t.DestinationLine1, DestinationLine2: t.DestinationLine2, DestinationCity: t.DestinationCity, DestinationRegion: t.DestinationRegion, DestinationPostalCode: t.DestinationPostalCode, CountryCode: t.CountryCode, BusinessContactName: t.BusinessContactName, BusinessContactPhone: t.BusinessContactPhone, WindowStart: t.WindowStart, WindowEnd: t.WindowEnd, PackageCount: t.PackageCount, HandlingNotes: t.HandlingNotes, AssignmentExpiresAt: t.AssignmentExpiresAt, AssignmentGeneration: t.AssignmentGeneration, Status: t.Status, Version: t.Version, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, AllowedActions: actions}
}

func controlDelivery(t *deliveryTask) {
	t.AllowedActions = []string{}
	switch t.Status {
	case "draft", "assigned":
		t.AllowedActions = []string{"edit", "cancel"}
	case "in_transit", "exception":
		t.AllowedActions = []string{"reassign", "cancel"}
	}
}

func registerDeliveryRoutes(g *platform.Router, h *handlers) {
	base := "/parties/:party/premises/:premise"
	platform.ListRoute(g, base+"/delivery-tasks", PermDeliveryRead, h.listDeliveryTasks)
	g.GET(base+"/delivery-tasks/:delivery", PermDeliveryRead, h.getDeliveryTask)
	g.PUT(base+"/delivery-tasks/:delivery", PermDeliveryWrite, h.putDeliveryTask)
	platform.ListRoute(g, base+"/delivery-tasks/:delivery/revisions", PermDeliveryRead, h.listDeliveryRevisions)
	platform.ListRoute(g, base+"/delivery-drivers", PermDeliveryWrite, h.listDeliveryDrivers)
	platform.ListRoute(g, base+"/my-deliveries", PermDriverRead, h.listDriverDeliveries)
	g.GET(base+"/my-deliveries/:delivery", PermDriverRead, h.getDriverDelivery)
	g.PUT(base+"/my-deliveries/:delivery/actions/:action", PermDriverAction, h.putDriverAction)
}

func normalizeDelivery(in *deliveryTaskInput) bool {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	if !cleanText(&in.SourceReference, &in.DestinationBusinessName, &in.DestinationLine1, &in.DestinationLine2, &in.DestinationCity, &in.DestinationRegion, &in.DestinationPostalCode, &in.BusinessContactName, &in.BusinessContactPhone, &in.HandlingNotes, &in.ChangeReason) || !validDeliveryTaskFieldsPatterns(in.DeliveryTaskFields) {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, in.WindowStart)
	if err != nil {
		return false
	}
	end, err := time.Parse(time.RFC3339Nano, in.WindowEnd)
	if err != nil || !end.After(start) {
		return false
	}
	in.WindowStart = start.UTC().Truncate(time.Microsecond).Format(deliveryTimeFormat)
	in.WindowEnd = end.UTC().Truncate(time.Microsecond).Format(deliveryTimeFormat)
	if in.Status == "draft" || in.Status == "cancelled" {
		in.DriverSubject = ""
		in.AssignmentExpiresAt = ""
		return true
	}
	expires, err := time.Parse(time.RFC3339Nano, in.AssignmentExpiresAt)
	if err != nil || !subjectID.MatchString(in.DriverSubject) {
		return false
	}
	in.AssignmentExpiresAt = expires.UTC().Truncate(time.Microsecond).Format(deliveryTimeFormat)
	return true
}

func deliveryError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && strings.HasPrefix(pg.ConstraintName, "supply_delivery_") {
		if pg.Code == "23503" || pg.Code == "42501" {
			respond.NotFound(c, "delivery task")
			return
		}
		if pg.Code == "23514" || pg.Code == "23505" {
			respond.Conflict(c, "The delivery task, assignment, source or transition has changed. Refresh and retry.")
			return
		}
	}
	structureError(c, err)
}

func deliveryValues(t deliveryTask) map[string]any {
	var expires any
	if t.AssignmentExpiresAt != "" {
		expires = t.AssignmentExpiresAt
	}
	return map[string]any{"code": t.Code, "source_kind": "manual_logistics", "source_reference": t.SourceReference, "destination_business_name": t.DestinationBusinessName, "destination_line1": t.DestinationLine1, "destination_line2": t.DestinationLine2, "destination_city": t.DestinationCity, "destination_region": t.DestinationRegion, "destination_postal_code": t.DestinationPostalCode, "country_code": t.CountryCode, "business_contact_name": t.BusinessContactName, "business_contact_phone": t.BusinessContactPhone, "window_start": t.WindowStart, "window_end": t.WindowEnd, "package_count": t.PackageCount, "handling_notes": t.HandlingNotes, "driver_subject": t.DriverSubject, "assignment_expires_at": expires, "assignment_generation": t.AssignmentGeneration, "status": t.Status, "version": t.Version}
}

func (h *handlers) putDeliveryTask(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "delivery")
	if !ok {
		return
	}
	var in deliveryTaskInput
	if !decode(c, &in) {
		return
	}
	if !normalizeDelivery(&in) || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Use valid business logistics fields, dates and a reason. No patient details are permitted.")
		return
	}
	var saved deliveryTask
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := lockAccessAdministration(tx, p, partyID); err != nil {
			return err
		}
		parent, err := operatingParent(tx, p, partyID, premiseID, deliveryControllers)
		if err != nil {
			return err
		}
		if parent.CountryCode != in.CountryCode {
			return errInvalidRegistry
		}
		if err = lockRegistry(tx, p, partyID, id, "delivery:"+premiseID.String()); err != nil {
			return err
		}
		err = tx.Select(deliveryColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		replay, err := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.DeliveryTaskFields, in.DeliveryTaskFields))
		if err != nil || replay {
			return err
		}
		if !exists {
			saved = deliveryTask{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID, PremiseID: premiseID, SourceKind: "manual_logistics"}
		}
		if saved.DriverSubject != in.DriverSubject || saved.AssignmentExpiresAt != in.AssignmentExpiresAt {
			saved.AssignmentGeneration++
		}
		saved.DeliveryTaskFields = in.DeliveryTaskFields
		saved.Version++
		values := deliveryValues(saved)
		query := tx.Table("supply_delivery_tasks").Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id)
		if exists {
			err = query.Updates(values).Error
		} else {
			values["tenant_id"] = p.TenantID
			values["party_id"] = partyID
			values["premise_id"] = premiseID
			values["id"] = id
			err = tx.Table("supply_delivery_tasks").Create(values).Error
		}
		if err != nil {
			return err
		}
		if err = tx.Select(deliveryColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error; err != nil {
			return err
		}
		return h.recordDelivery(tx, p, saved, in.ChangeReason, "control")
	})
	if err != nil {
		deliveryError(c, err)
		return
	}
	controlDelivery(&saved)
	respond.OK(c, saved)
}

func (h *handlers) recordDelivery(tx *gorm.DB, p authn.Principal, saved deliveryTask, reason, operation string) error {
	snapshot, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err = tx.Exec(`INSERT INTO supply_delivery_revisions(id,tenant_id,party_id,premise_id,delivery_id,version,snapshot,actor_subject,reason) VALUES(?,?,?,?,?,?,?,?,?)`, id, p.TenantID, saved.PartyID, saved.PremiseID, saved.ID, saved.Version, string(snapshot), p.Subject, reason).Error; err != nil {
		return err
	}
	data, err := json.Marshal(supplycontract.DeliveryTaskChangedData{PartyID: saved.PartyID.String(), PremiseID: saved.PremiseID.String(), DeliveryID: saved.ID.String(), Version: saved.Version, Operation: operation})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, supplycontract.SubjectDeliveryTaskChanged, events.Event{Type: supplycontract.SubjectDeliveryTaskChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) getDeliveryTask(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "delivery")
	if !ok {
		return
	}
	var saved deliveryTask
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		parent, err := operatingHistoryParent(tx, p, partyID, premiseID, deliveryReaders)
		if err != nil {
			return err
		}
		if err := tx.Select(deliveryColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error; err != nil {
			return err
		}
		allowed, err := operatingAllowed(tx, p, partyID, premiseID, deliveryControllers)
		saved.AllowedActions = []string{}
		if allowed && parent.Status == "active" {
			controlDelivery(&saved)
		}
		return err
	})
	if err != nil {
		deliveryError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listDeliveryTasks(c *gin.Context, params pagination.Params) ([]deliveryTask, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	var rows []deliveryTask
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		parent, err := operatingHistoryParent(tx, p, partyID, premiseID, deliveryReaders)
		if err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(deliveryColumns).Where("tenant_id=? AND party_id=? AND premise_id=?", p.TenantID, partyID, premiseID).Find(&rows).Error; err != nil {
			return err
		}
		allowed, err := operatingAllowed(tx, p, partyID, premiseID, deliveryControllers)
		for i := range rows {
			rows[i].AllowedActions = []string{}
			if allowed && parent.Status == "active" {
				controlDelivery(&rows[i])
			}
		}
		return err
	})
	if err != nil {
		deliveryError(c, err)
	}
	return rows, nil
}

func (h *handlers) listDeliveryRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "delivery")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingHistoryParent(tx, p, partyID, premiseID, deliveryReaders); err != nil {
			return err
		}
		var head deliveryTask
		if err := tx.Select("id").Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&head).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_delivery_revisions").Select("id,party_id,delivery_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND premise_id=? AND delivery_id=?", p.TenantID, partyID, premiseID, id).Find(&rows).Error
	})
	if err != nil {
		deliveryError(c, err)
	}
	return rows, nil
}

type deliveryDriverCandidate struct {
	ID        uuid.UUID `json:"id"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

func (r deliveryDriverCandidate) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *handlers) listDeliveryDrivers(c *gin.Context, params pagination.Params) ([]deliveryDriverCandidate, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	var rows []deliveryDriverCandidate
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, deliveryControllers); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_delivery_drivers(?,?,?) AS d", p.TenantID, partyID, premiseID).Select("id,subject,created_at").Find(&rows).Error
	})
	if err != nil {
		deliveryError(c, err)
	}
	return rows, nil
}

func loadAssignedDelivery(tx *gorm.DB, p authn.Principal, partyID, premiseID, id uuid.UUID, columns string, saved *deliveryTask) error {
	return tx.Select(columns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=? AND driver_subject=? AND assignment_expires_at>clock_timestamp()", p.TenantID, partyID, premiseID, id, p.Subject).First(saved).Error
}

func (h *handlers) getDriverDelivery(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "delivery")
	if !ok {
		return
	}
	var saved deliveryTask
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, []string{"driver"}); err != nil {
			return err
		}
		return loadAssignedDelivery(tx, p, partyID, premiseID, id, driverColumns, &saved)
	})
	if err != nil {
		deliveryError(c, err)
		return
	}
	respond.OK(c, projectDriver(saved))
}

func (h *handlers) listDriverDeliveries(c *gin.Context, params pagination.Params) ([]driverDeliveryTask, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	var tasks []deliveryTask
	rows := []driverDeliveryTask{}
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, []string{"driver"}); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(driverColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND driver_subject=? AND assignment_expires_at>clock_timestamp()", p.TenantID, partyID, premiseID, p.Subject).Find(&tasks).Error
	})
	if err != nil {
		deliveryError(c, err)
	}
	for _, task := range tasks {
		rows = append(rows, projectDriver(task))
	}
	return rows, nil
}

func (h *handlers) putDriverAction(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "delivery")
	if !ok {
		return
	}
	command, ok := resourceID(c, "action")
	if !ok {
		return
	}
	var in deliveryActionInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(&in.Reason) || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Choose a valid delivery action and reason.")
		return
	}
	payload, err := json.Marshal(in)
	if err != nil {
		deliveryError(c, err)
		return
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	var result driverDeliveryTask
	err = h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := lockAccessAdministration(tx, p, partyID); err != nil {
			return err
		}
		if _, err := operatingParent(tx, p, partyID, premiseID, []string{"driver"}); err != nil {
			return err
		}
		if err := lockRegistry(tx, p, partyID, id, "delivery:"+premiseID.String()); err != nil {
			return err
		}
		var saved deliveryTask
		if err := loadAssignedDelivery(tx, p, partyID, premiseID, id, deliveryColumns, &saved); err != nil {
			return err
		}
		if saved.AssignmentGeneration != in.ExpectedAssignmentGeneration {
			return errConflict
		}
		var previous struct {
			RequestHash string
			Response    json.RawMessage
		}
		err := tx.Table("supply_delivery_commands").Select("request_hash,response").Where("tenant_id=? AND party_id=? AND premise_id=? AND delivery_id=? AND id=?", p.TenantID, partyID, premiseID, id, command).First(&previous).Error
		if err == nil {
			if previous.RequestHash != hash {
				return errConflict
			}
			return json.Unmarshal(previous.Response, &result)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if saved.Version != *in.ExpectedVersion {
			return errConflict
		}
		next := ""
		if saved.Status == "assigned" && in.Action == "start" {
			next = "in_transit"
		}
		if saved.Status == "in_transit" && in.Action == "deliver" {
			next = "delivered"
		}
		if (saved.Status == "assigned" || saved.Status == "in_transit") && in.Action == "exception" {
			next = "exception"
		}
		if next == "" {
			return errConflict
		}
		if err = tx.Table("supply_delivery_tasks").Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).Updates(map[string]any{"status": next, "version": saved.Version + 1}).Error; err != nil {
			return err
		}
		if err = tx.Select(deliveryColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error; err != nil {
			return err
		}
		if err = h.recordDelivery(tx, p, saved, in.Reason, in.Action); err != nil {
			return err
		}
		result = projectDriver(saved)
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO supply_delivery_commands(tenant_id,party_id,premise_id,delivery_id,id,request_hash,response) VALUES(?,?,?,?,?,?,?)`, p.TenantID, partyID, premiseID, id, command, hash, string(response)).Error
	})
	if err != nil {
		deliveryError(c, err)
		return
	}
	respond.OK(c, result)
}
