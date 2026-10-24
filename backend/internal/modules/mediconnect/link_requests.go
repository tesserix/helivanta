package mediconnect

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type linkRequest struct {
	ID           uuid.UUID  `json:"id"`
	HospitalID   uuid.UUID  `json:"hospital_id"`
	PatientID    uuid.UUID  `json:"patient_id"`
	Relationship string     `json:"relationship"`
	Status       string     `json:"status"`
	Version      int64      `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	WithdrawnAt  *time.Time `json:"withdrawn_at"`
}

func (linkRequest) TableName() string                 { return "mediconnect_link_requests" }
func (r linkRequest) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const linkColumns = "id, hospital_id, patient_id, relationship, status, version, created_at, updated_at, withdrawn_at"

var errLinkConflict = errors.New("link request conflicts with recorded state")

type linkInput struct {
	HospitalID   uuid.UUID `json:"hospital_id"`
	PatientID    uuid.UUID `json:"patient_id"`
	Relationship string    `json:"relationship"`
}

func linkID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("request"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "link request")
		return uuid.Nil, false
	}
	return id, true
}

func validRelationship(value string) bool {
	switch value {
	case "self", "child", "parent", "spouse", "sibling", "ward", "other":
		return true
	}
	return false
}

func readLink(tx *gorm.DB, p authn.Principal, id uuid.UUID, row *linkRequest) error {
	return tx.Select(linkColumns).Where("tenant_id = ? AND subject = ? AND id = ?", p.TenantID, p.Subject, id).Take(row).Error
}

func auditLinkRead(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events (tenant_id, actor_subject, action, resource_type, resource_id)
 VALUES (?, ?, 'read', 'mediconnect_link_requests', ?)`, p.TenantID, p.Subject, id).Error
}

func linkError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "link request")
	case errors.Is(err, errLinkConflict):
		respond.Conflict(c, "Link request conflicts with recorded state")
	default:
		// Database errors can contain claimed patient identifiers; keep them out of logs.
		respond.InternalErr(c, errors.New("link request persistence failed"), "Link request operation unavailable")
	}
	c.Abort()
}

func (h *accountHandlers) submitLinkRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := linkID(c)
	if !ok {
		return
	}
	var input linkInput
	if !decode(c, &input) {
		return
	}
	if input.HospitalID == uuid.Nil || input.PatientID == uuid.Nil || !validRelationship(input.Relationship) {
		respond.Error(c, 400, "invalid_request", "Provide hospital, patient and relationship claims")
		return
	}
	var row linkRequest
	created := false
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		var owner account
		if err := readAccount(tx, p, &owner); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO mediconnect_link_requests (tenant_id, id, account_id, subject, hospital_id, patient_id, relationship)
   VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tenant_id, id) DO NOTHING`, p.TenantID, id, owner.ID, p.Subject, input.HospitalID, input.PatientID, input.Relationship)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readLink(tx, p, id, &row); err != nil {
			return err
		}
		if row.HospitalID != input.HospitalID || row.PatientID != input.PatientID || row.Relationship != input.Relationship {
			return errLinkConflict
		}
		if !created {
			return auditLinkRead(tx, p, id)
		}
		return nil
	})
	if err != nil {
		linkError(c, err)
		return
	}
	if created {
		respond.Accepted(c, row)
	} else {
		respond.OK(c, row)
	}
}

func (h *accountHandlers) linkTransaction(c *gin.Context, p authn.Principal, fn func(*gorm.DB, account) error) error {
	return h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := setActor(tx, p.Subject); err != nil {
			return err
		}
		var owner account
		if err := readAccount(tx, p, &owner); err != nil {
			return err
		}
		return fn(tx, owner)
	})
}

func (h *accountHandlers) getLinkRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := linkID(c)
	if !ok {
		return
	}
	var row linkRequest
	err := h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		if err := readLink(tx, p, id, &row); err != nil {
			return err
		}
		return auditLinkRead(tx, p, id)
	})
	if err != nil {
		linkError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *accountHandlers) listLinkRequests(c *gin.Context, params pagination.Params) ([]linkRequest, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []linkRequest
	err := h.linkTransaction(c, p, func(tx *gorm.DB, owner account) error {
		query := tx.Select(linkColumns).Where("tenant_id = ? AND subject = ?", p.TenantID, p.Subject)
		if err := tenantdb.ApplyKeyset(query, params).Find(&rows).Error; err != nil {
			return err
		}
		if err := auditRead(tx, p, owner.ID); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return tx.Exec(`INSERT INTO audit_events (tenant_id, actor_subject, action, resource_type, resource_id)
   SELECT tenant_id, ?, 'read', 'mediconnect_link_requests', id FROM mediconnect_link_requests
   WHERE tenant_id = ? AND subject = ? AND id IN ?`, p.Subject, p.TenantID, p.Subject, ids).Error
	})
	if err != nil {
		linkError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *accountHandlers) withdrawLinkRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := linkID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decode(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion > 9007199254740991 {
		respond.Error(c, 400, "invalid_request", "Provide an expected version")
		return
	}
	var row linkRequest
	err := h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		if err := readLink(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, id, &row); err != nil {
			return err
		}
		if row.Status == "withdrawn" {
			if input.ExpectedVersion != row.Version && input.ExpectedVersion != row.Version-1 {
				return errLinkConflict
			}
			if err := h.withdrawReview(tx, p, row); err != nil {
				return err
			}
			return auditLinkRead(tx, p, id)
		}
		if row.Version != input.ExpectedVersion {
			return errLinkConflict
		}
		result := tx.Exec(`UPDATE mediconnect_link_requests SET status = 'withdrawn', version = 2,
   withdrawn_at = clock_timestamp(), updated_at = clock_timestamp()
   WHERE tenant_id = ? AND subject = ? AND id = ? AND status = 'pending_verification' AND version = ?`,
			p.TenantID, p.Subject, id, input.ExpectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if err := readLink(tx, p, id, &row); err != nil {
			return err
		}
		if row.Status != "withdrawn" {
			return errLinkConflict
		}
		if result.RowsAffected == 0 {
			return auditLinkRead(tx, p, id)
		}
		return h.withdrawReview(tx, p, row)
	})
	if err != nil {
		linkError(c, err)
		return
	}
	respond.OK(c, row)
}
