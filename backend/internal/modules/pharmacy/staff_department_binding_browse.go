package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r StaffDepartmentBinding) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (r StaffDepartmentBindingHistoryEntry) PageKey() (time.Time, uuid.UUID) {
	return r.RecordedAt, r.ID
}

func bindingPageQuery(c *gin.Context, page pagination.Params) bool {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.BadRequest(c, errors.New("invalid relationship query"))
		return false
	}
	for key, values := range query {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, errors.New("provide one limit and an optional cursor"))
			return false
		}
	}
	if page.Limit > 50 {
		respond.BadRequest(c, errors.New("relationship lists are limited to 50 per page"))
		return false
	}
	return true
}

func (h *staffDepartmentBindingHandlers) list(c *gin.Context, page pagination.Params) ([]StaffDepartmentBinding, error) {
	if !bindingPageQuery(c, page) {
		return nil, nil
	}
	s, ok := bindingBaseScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffDepartmentBinding{}
	err := h.command(c, s, PermStaffBindingRead, false, func(_ context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		if err := h.outlet(tx, s, false); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, page).Table("pharmacy_staff_department_bindings").Select("*, ?::uuid AS pharmacy_id", s.pharmacy).Where("tenant_id=? AND outlet_id=? AND hospital_id=?", s.principal.TenantID, s.outlet, s.hospital).Find(&rows).Error; err != nil {
			return err
		}
		var at time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
			return err
		}
		return auditStaffBinding(tx, s, actor, "read", nil, at, map[string]any{"record_count": min(len(rows), page.Limit), "projection": "recorded_relationships"})
	})
	if err != nil {
		staffBindingError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *staffDepartmentBindingHandlers) history(c *gin.Context, page pagination.Params) ([]StaffDepartmentBindingHistoryEntry, error) {
	if !bindingPageQuery(c, page) {
		return nil, nil
	}
	s, ok := bindingRequestScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffDepartmentBindingHistoryEntry{}
	err := h.command(c, s, PermStaffBindingRead, false, func(_ context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		if err := h.outlet(tx, s, false); err != nil {
			return err
		}
		head, err := readStaffBinding(tx, s, false)
		if err != nil {
			return err
		}
		var stored []struct {
			StaffDepartmentBindingHistoryEntry
			AuthorizationJSON []byte `gorm:"column:authorization_json"`
		}
		query := tenantdb.ApplyOrderedKeyset(tx, page, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.NewestFirst}).Table("pharmacy_staff_binding_audit").
			Select(`id,binding_id,outlet_id,hospital_id,actor_subject,action,command_id,recorded_at,COALESCE((changes->>'version')::bigint,(changes->>'receipt_version')::bigint) AS recorded_version,changes->>'source_state' AS source_state,(changes->>'related_binding_id')::uuid AS related_binding_id,(changes->>'related_binding_version')::bigint AS related_binding_version,changes->'authorizations' AS authorization_json`).
			Where("tenant_id=? AND outlet_id=? AND hospital_id=? AND binding_id=?", s.principal.TenantID, s.outlet, s.hospital, s.binding)
		if err := query.Find(&stored).Error; err != nil {
			return err
		}
		for _, record := range stored {
			row := record.StaffDepartmentBindingHistoryEntry
			if err := json.Unmarshal(record.AuthorizationJSON, &row.Authorizations); err != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if row.RecordedVersion < 1 || row.RecordedVersion > 9007199254740990 || len(row.Authorizations) < 2 || len(row.Authorizations) > authz.MaxScopedCommandChecks {
				return authz.ErrScopedAuthorizationUnavailable
			}
			rows = append(rows, row)
		}
		var at time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
			return err
		}
		return auditStaffBinding(tx, s, actor, "read", nil, at, map[string]any{"version": head.Version, "record_count": min(len(rows), page.Limit), "projection": "relationship_history"})
	})
	if err != nil {
		staffBindingError(c, err)
		return nil, nil
	}
	return rows, nil
}
