package privacy

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type command struct {
	ID               uuid.UUID `json:"id"`
	RequestID        uuid.UUID `json:"request_id"`
	ActorSubject     string    `json:"-"`
	Action           string    `json:"action"`
	Message          string    `json:"message"`
	ExpectedVersion  int64     `json:"expected_version"`
	ResultingVersion int64     `json:"resulting_version"`
	CreatedAt        time.Time `json:"created_at"`
}

func (command) TableName() string                 { return "privacy_commands" }
func (c command) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }
func (r request) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const commandColumns = "id,request_id,actor_subject,action,message,expected_version,resulting_version,created_at"

var errForbidden = errors.New("privacy action forbidden")

func transition(row request, actor, action string) (string, error) {
	if row.Status == "responded" || row.Status == "withdrawn" {
		return "", errConflict
	}
	own := actor == row.Subject
	if action == "withdraw" {
		if !own {
			return "", errForbidden
		}
		return "withdrawn", nil
	}
	if action == "reply" {
		if !own {
			return "", errForbidden
		}
		if row.Status == "awaiting_principal" {
			return "under_review", nil
		}
		return "", errConflict
	}
	if own {
		return "", errForbidden
	}
	switch action {
	case "start_review":
		if row.Status == "received" {
			return "under_review", nil
		}
	case "request_information":
		if row.Status == "under_review" {
			return "awaiting_principal", nil
		}
	case "respond":
		if row.Status == "under_review" {
			return "responded", nil
		}
	}
	return "", errConflict
}
func (h *handlers) command(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "request")
	if !ok {
		return
	}
	cid, ok := pathID(c, "command")
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expected_version"`
		Action          string `json:"action"`
		Message         string `json:"message"`
	}
	if !decode(c, &in) {
		return
	}
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 1000000 || !validText(in.Message, 4096) || (in.Action != "start_review" && in.Action != "request_information" && in.Action != "reply" && in.Action != "respond" && in.Action != "withdraw") {
		invalid(c)
		return
	}
	var out command
	created := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		if e := allowOfficer(tx, p); e != nil {
			return e
		}
		var row request
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(requestColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; e != nil {
			return e
		}
		found := tx.Select(commandColumns).Where("tenant_id=? AND id=? AND request_id=?", p.TenantID, cid, id).Take(&out).Error
		if found == nil {
			if out.ActorSubject != p.Subject || out.Action != in.Action || out.Message != in.Message || out.ExpectedVersion != in.ExpectedVersion {
				return errConflict
			}
			return audit(tx, p, "privacy_commands", cid)
		}
		if !errors.Is(found, gorm.ErrRecordNotFound) {
			return found
		}
		next, e := transition(row, p.Subject, in.Action)
		if e != nil {
			return e
		}
		if row.Version != in.ExpectedVersion {
			return errConflict
		}
		if e := tx.Exec(`INSERT INTO privacy_commands(tenant_id,id,request_id,actor_subject,action,message,expected_version,resulting_version) VALUES (?,?,?,?,?,?,?,?)`, p.TenantID, cid, id, p.Subject, in.Action, in.Message, in.ExpectedVersion, in.ExpectedVersion+1).Error; e != nil {
			return e
		}
		result := tx.Exec("UPDATE privacy_requests SET status=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND id=? AND version=? AND status=?", next, p.TenantID, id, in.ExpectedVersion, row.Status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errConflict
		}
		created = true
		if e := tx.Select(commandColumns).Where("tenant_id=? AND id=?", p.TenantID, cid).Take(&out).Error; e != nil {
			return e
		}
		return audit(tx, p, "privacy_commands", cid)
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, out)
	} else {
		respond.OK(c, out)
	}
}
func (h *handlers) list(c *gin.Context, params pagination.Params) ([]request, error) {
	return h.requests(c, params, false)
}
func (h *handlers) queue(c *gin.Context, params pagination.Params) ([]request, error) {
	return h.requests(c, params, true)
}
func (h *handlers) requests(c *gin.Context, params pagination.Params, queue bool) ([]request, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []request
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		if queue {
			var o officer
			if e := actorOfficer(tx, p, &o); e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return errForbidden
				}
				return e
			}
			if o.Subject != p.Subject {
				return errForbidden
			}
			if e := allowOfficer(tx, p); e != nil {
				return e
			}
		}
		q := tenantdb.ApplyKeyset(tx, params).Select(requestColumns).Where("tenant_id=?", p.TenantID)
		if !queue {
			q = q.Where("subject=?", p.Subject)
		}
		if e := q.Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditMany(tx, p, "privacy_requests", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) history(c *gin.Context, params pagination.Params) ([]command, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	id, ok := pathID(c, "request")
	if !ok {
		return nil, nil
	}
	var rows []command
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := scope(tx, p, false); e != nil {
			return e
		}
		if e := allowOfficer(tx, p); e != nil {
			return e
		}
		var row request
		if e := tx.Select(requestColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; e != nil {
			return e
		}
		if e := tenantdb.ApplyKeyset(tx, params).Select(commandColumns).Where("tenant_id=? AND request_id=?", p.TenantID, id).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditMany(tx, p, "privacy_commands", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

func (c *command) AfterFind(*gorm.DB) error { c.CreatedAt = c.CreatedAt.UTC(); return nil }
