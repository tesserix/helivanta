package emergency

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type receivingRequest struct {
	Usable               bool       `json:"usable"`
	HandoverReady        bool       `json:"handover_ready"`
	ArrivalRecorded      bool       `json:"arrival_recorded"`
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"provider_tenant_id"`
	BaseID               uuid.UUID  `json:"base_id"`
	DispatchID           uuid.UUID  `json:"dispatch_id"`
	DispatchVersion      int64      `json:"dispatch_version"`
	LinkID               uuid.UUID  `json:"link_id"`
	PreviousRequestID    *uuid.UUID `json:"previous_request_id"`
	DestinationTenantID  uuid.UUID  `json:"destination_tenant_id"`
	FacilityID           uuid.UUID  `json:"facility_id"`
	Sequence             int64      `json:"sequence"`
	RequiredClass        string     `json:"required_class"`
	ResponseSeconds      int        `json:"response_seconds"`
	ResponseDeadline     time.Time  `json:"response_deadline"`
	EvidenceReference    string     `json:"evidence_reference"`
	RequestedBy          string     `json:"requested_by"`
	Status               string     `json:"status"`
	Version              int64      `json:"version"`
	AcceptanceValidUntil *time.Time `json:"acceptance_valid_until"`
	DestinationName      *string    `json:"destination_name"`
	Superseded           bool       `json:"superseded"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

const receivingRequestColumns = "id,tenant_id,base_id,dispatch_id,dispatch_version,link_id,previous_request_id,destination_tenant_id,facility_id,sequence,required_class,response_seconds,response_deadline,evidence_reference,requested_by,status,version,acceptance_valid_until,destination_name,superseded,created_at,updated_at"

func sameOptionalID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func (h *handlers) putReceivingRequest(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	d, ok := resourceID(c, "dispatch")
	if !ok {
		return
	}
	id, ok := resourceID(c, "request")
	if !ok {
		return
	}
	var in ReceivingRequestInput
	if !decode(c, &in) {
		return
	}
	if in.LinkID.Version() != 7 || !policyShape.MatchString(in.EvidenceReference) || (in.PreviousRequestID != nil && in.PreviousRequestID.Version() != 7) {
		respond.Error(c, 400, "invalid_request", "invalid receiving request")
		return
	}
	var out receivingRequest
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "intake"); err != nil {
			return err
		}
		if err := clinicalBaseLock(tx, p, b, "NO KEY UPDATE"); err != nil {
			return err
		}
		var trip dispatch
		if err := tx.Select(dispatchColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, d).Take(&trip).Error; err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_request_status").Select(receivingRequestColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Take(&out).Error
		if err == nil {
			if out.DispatchID != d || out.DispatchVersion != in.Version || out.LinkID != in.LinkID || out.ResponseSeconds != in.ResponseSeconds || out.EvidenceReference != in.EvidenceReference || !sameOptionalID(out.PreviousRequestID, in.PreviousRequestID) || out.RequestedBy != p.Subject {
				return errCaseConflict
			}
			out.Status = "pending"
			out.Version = 1
			out.AcceptanceValidUntil = nil
			out.DestinationName = nil
			out.Superseded = false
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var previous struct{ ID uuid.UUID }
		if err := tx.Table("emergency_receiving_requests").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND dispatch_id=?", p.TenantID, b, d).Order("sequence DESC").Limit(1).Find(&previous).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_requests(tenant_id,base_id,id,dispatch_id,dispatch_version,link_id,previous_request_id,response_seconds,evidence_reference,requested_by)VALUES(?,?,?,?,?,?,?,?,?,?)`, p.TenantID, b, id, d, in.Version, in.LinkID, in.PreviousRequestID, in.ResponseSeconds, in.EvidenceReference, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Table("emergency_receiving_request_status").Select(receivingRequestColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Take(&out).Error; err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: b.String(), DispatchID: d.String(), Version: trip.Version})
		if err != nil {
			return err
		}
		return transportEvent(tx, p, emergencycontract.SubjectDispatchChanged, data)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) getReceivingRequest(c *gin.Context) {
	p, s, provider, id, ok := receivingRecordScope(c, "request")
	if !ok {
		return
	}
	b := s
	recipient := c.Param("facility") != ""
	if recipient {
		b = uuid.Nil
	}
	var out receivingRequest
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		query := tx.Table("emergency_receiving_request_status").Select(receivingRequestColumns).Where("tenant_id=? AND id=?", provider, id)
		if recipient {
			query = query.Where("destination_tenant_id=? AND facility_id=?", p.TenantID, s)
		} else {
			d, ok := resourceID(c, "dispatch")
			if !ok {
				return gorm.ErrRecordNotFound
			}
			if err := receivingProviderRead(tx, p, s, d); err != nil {
				return err
			}
			query = query.Where("base_id=? AND dispatch_id=?", s, d)
		}
		if err := query.Take(&out).Error; err != nil {
			return err
		}
		if recipient {
			return tx.Raw(`SELECT emergency_receiving_arrived(?::uuid,?::uuid,?::uuid,?::uuid)`, provider, out.BaseID, out.DispatchID, out.ID).Scan(&out.ArrivalRecorded).Error
		}
		if out.Superseded {
			return nil
		}
		var flags struct {
			Usable        bool
			HandoverReady bool
		}
		if err := tx.Raw(`SELECT emergency_receiving_ready(?::uuid,?::uuid,?::uuid,false,?::uuid)AS usable,emergency_receiving_ready(?::uuid,?::uuid,?::uuid,true,?::uuid)AS handover_ready`, provider, out.BaseID, out.DispatchID, out.ID, provider, out.BaseID, out.DispatchID, out.ID).Scan(&flags).Error; err != nil {
			return err
		}
		out.Usable = flags.Usable
		out.HandoverReady = flags.HandoverReady
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}

type receivingCommand struct {
	ID                   uuid.UUID  `json:"id"`
	RequestID            uuid.UUID  `json:"request_id"`
	ExpectedVersion      int64      `json:"expected_version"`
	Operation            string     `json:"operation"`
	EvidenceReference    string     `json:"evidence_reference"`
	ValidUntil           *time.Time `json:"valid_until"`
	ActorTenantID        uuid.UUID  `json:"actor_tenant_id"`
	ActorSubject         string     `json:"actor_subject"`
	Status               string     `json:"status" gorm:"column:result_status"`
	Version              int64      `json:"version" gorm:"column:result_version"`
	AcceptanceValidUntil *time.Time `json:"acceptance_valid_until" gorm:"column:result_valid_until"`
	CreatedAt            time.Time  `json:"created_at"`
}

const receivingCommandColumns = "id,request_id,expected_version,operation,evidence_reference,valid_until,actor_tenant_id,actor_subject,result_status,result_version,result_valid_until,created_at"

func equalOptionalTime(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}
func (h *handlers) putReceivingCommand(c *gin.Context) {
	p, f, provider, id, ok := receivingRecordScope(c, "request")
	if !ok {
		return
	}
	command, ok := resourceID(c, "command")
	if !ok {
		return
	}
	var in ReceivingCommandInput
	if !decode(c, &in) {
		return
	}
	if !policyShape.MatchString(in.EvidenceReference) {
		respond.Error(c, 400, "invalid_request", "operational communication reference required")
		return
	}
	if in.ValidUntil != nil {
		truncated := in.ValidUntil.Truncate(time.Microsecond)
		in.ValidUntil = &truncated
	}
	var out receivingCommand
	err := h.transaction(c.Request.Context(), p, uuid.Nil, false, func(tx *gorm.DB) error {
		var request receivingRequest
		if err := tx.Table("emergency_receiving_request_status").Select(receivingRequestColumns).Where("tenant_id=? AND id=? AND destination_tenant_id=? AND facility_id=?", provider, id, p.TenantID, f).Take(&request).Error; err != nil {
			return err
		}
		var locked struct{ ID uuid.UUID }
		if err := tx.Table("emergency_receiving_requests").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", provider, request.BaseID, id).Take(&locked).Error; err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_commands").Select(receivingCommandColumns).Where("tenant_id=? AND base_id=? AND id=?", provider, request.BaseID, command).Take(&out).Error
		if err == nil {
			if out.RequestID != id || out.ExpectedVersion != in.Version || out.Operation != in.Operation || out.EvidenceReference != in.EvidenceReference || !equalOptionalTime(out.ValidUntil, in.ValidUntil) || out.ActorSubject != p.Subject || out.ActorTenantID.String() != p.TenantID {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_commands(tenant_id,base_id,request_id,id,expected_version,operation,evidence_reference,valid_until,actor_tenant_id,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,?)`, provider, request.BaseID, id, command, in.Version, in.Operation, in.EvidenceReference, in.ValidUntil, p.TenantID, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Table("emergency_receiving_commands").Select(receivingCommandColumns).Where("tenant_id=? AND base_id=? AND id=?", provider, request.BaseID, command).Take(&out).Error; err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.ReceivingChangedData{BaseID: request.BaseID.String(), DispatchID: request.DispatchID.String(), RequestID: id.String(), Version: out.Version})
		if err != nil {
			return err
		}
		eventID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return events.Enqueue(tx, emergencycontract.SubjectReceivingChanged, events.Event{ID: eventID.String(), Type: emergencycontract.SubjectReceivingChanged, Version: 1, OccurredAt: time.Now().UTC(), TenantID: p.TenantID, DestinationTenantID: provider.String(), Data: data})
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
