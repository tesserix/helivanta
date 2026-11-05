package pharmacy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func (h *staffDepartmentBindingHandlers) decide(c *gin.Context) {
	s, ok := bindingRequestScope(c)
	if !ok {
		return
	}
	var in StaffDepartmentBindingCommandInput
	if err := decodeBindingInput(c, &in); err != nil {
		respond.Error(c, 400, "invalid_request", "Provide one department binding command.")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !bindingV7(in.ID) || in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740989 || !bindingReason(in.Reason) || in.AttestDepartmentResponsibility != (in.Operation == "approve") {
		respond.Error(c, 400, "invalid_request", "Choose the exact relationship version, reason and approval attestation.")
		return
	}
	permission := PermStaffBindingReview
	switch in.Operation {
	case "approve", "reject", "end":
	case "withdraw":
		permission = PermStaffBindingPrepare
	default:
		respond.Error(c, 400, "invalid_request", "Choose a supported department binding operation.")
		return
	}
	hash, err := bindingIntentHash(s.principal.Subject, in)
	if err != nil {
		staffBindingError(c, err)
		return
	}
	var result StaffDepartmentBindingCommandReceipt
	err = h.command(c, s, permission, true, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		level, err := outletAccess(tx, s.principal, s.outlet, s.pharmacy, false)
		if err != nil {
			return err
		}
		if level != "editor" {
			return gorm.ErrRecordNotFound
		}
		retained, found, err := readStaffBindingReceipt(tx, s, in.ID, hash)
		if err != nil {
			return err
		}
		if found {
			if err := h.outlet(tx, s, true); err != nil {
				return err
			}
			result = retained
			return auditStaffBindingReceiptRead(tx, s, actor, retained)
		}
		observed, err := readStaffBinding(tx, s, false)
		if err != nil {
			return err
		}
		apply := func(held context.Context) error {
			return h.db.WithTenantCommand(held, s.principal.TenantID, func(tx *gorm.DB) error {
				if err := h.outlet(tx, s, true); err != nil {
					return err
				}
				receipt, exists, err := readStaffBindingReceipt(tx, s, in.ID, hash)
				if err != nil {
					return err
				}
				if exists {
					result = receipt
					return auditStaffBindingReceiptRead(tx, s, actor, receipt)
				}
				row, err := readStaffBinding(tx, s, true)
				if err != nil {
					return err
				}
				var at time.Time
				if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
					return err
				}
				result = StaffDepartmentBindingCommandReceipt{ID: in.ID, BindingID: s.binding, Operation: in.Operation, ExpectedVersion: in.ExpectedVersion, Reason: in.Reason, ActorSubject: s.principal.Subject, ActorStaffID: actor.StaffID, RecordedAt: at}
				if err := h.applyDecision(tx, s, actor, row, in, &result); err != nil {
					return err
				}
				result.Binding, err = readStaffBinding(tx, s, false)
				if err != nil {
					return err
				}
				return h.record(tx, s, actor, hash, &result)
			})
		}
		if in.Operation == "approve" {
			if observed.ProposedBySubject == s.principal.Subject || observed.ProposerStaffID == actor.StaffID {
				return errStaffBindingConflict
			}
			return h.departments.WithCurrentDepartment(ctx, authz.DepartmentReference{TenantID: s.principal.TenantID, HospitalID: s.hospital, DepartmentID: observed.DepartmentID, DepartmentRevision: observed.DepartmentRevisionID}, apply)
		}
		return apply(ctx)
	})
	if err != nil {
		staffBindingError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *staffDepartmentBindingHandlers) applyDecision(tx *gorm.DB, s staffBindingScope, actor authz.StaffActorReference, row StaffDepartmentBinding, in StaffDepartmentBindingCommandInput, receipt *StaffDepartmentBindingCommandReceipt) error {
	if row.Version != in.ExpectedVersion {
		return errStaffBindingConflict
	}
	status := ""
	switch in.Operation {
	case "approve", "reject":
		if row.Status != "pending" || row.ProposedBySubject == s.principal.Subject || row.ProposerStaffID == actor.StaffID {
			return errStaffBindingConflict
		}
		status = "rejected"
		if in.Operation == "approve" {
			status = "approved"
			if err := h.replaceCurrent(tx, s, actor, row, receipt); err != nil {
				return err
			}
		}
	case "withdraw":
		if row.Status != "pending" || row.ProposedBySubject != s.principal.Subject || row.ProposerStaffID != actor.StaffID {
			return errStaffBindingConflict
		}
		status = "withdrawn"
	case "end":
		if row.Status != "approved" {
			return errStaffBindingConflict
		}
		changed := tx.Exec(`UPDATE pharmacy_staff_department_bindings SET status='ended',version=version+1,ended_at=? WHERE tenant_id=? AND outlet_id=? AND hospital_id=? AND id=? AND version=? AND status='approved'`, receipt.RecordedAt, s.principal.TenantID, s.outlet, s.hospital, s.binding, row.Version)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return errStaffBindingConflict
		}
		return nil
	}
	changed := tx.Exec(`UPDATE pharmacy_staff_department_bindings SET status=?,version=version+1,decided_by_subject=?,reviewer_staff_id=?,decision_reason=?,decided_at=? WHERE tenant_id=? AND outlet_id=? AND hospital_id=? AND id=? AND version=? AND status='pending'`, status, s.principal.Subject, actor.StaffID, in.Reason, receipt.RecordedAt, s.principal.TenantID, s.outlet, s.hospital, s.binding, row.Version)
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return errStaffBindingConflict
	}
	return nil
}

func (h *staffDepartmentBindingHandlers) replaceCurrent(tx *gorm.DB, s staffBindingScope, actor authz.StaffActorReference, proposal StaffDepartmentBinding, receipt *StaffDepartmentBindingCommandReceipt) error {
	var current []struct {
		ID      uuid.UUID
		Version int64
	}
	if err := tx.Table("pharmacy_staff_department_bindings").Select("id,version").Where("tenant_id=? AND outlet_id=? AND hospital_id=? AND status='approved'", s.principal.TenantID, s.outlet, s.hospital).Limit(2).Find(&current).Error; err != nil {
		return err
	}
	if proposal.ReplacesBindingID == nil {
		if len(current) != 0 {
			return errStaffBindingConflict
		}
		return nil
	}
	if len(current) != 1 || current[0].ID != *proposal.ReplacesBindingID || current[0].Version != *proposal.ReplacesBindingVersion || current[0].Version >= 9007199254740990 {
		return errStaffBindingConflict
	}
	old := current[0]
	changed := tx.Exec(`UPDATE pharmacy_staff_department_bindings SET status='ended',version=version+1,ended_at=? WHERE tenant_id=? AND outlet_id=? AND hospital_id=? AND id=? AND version=? AND status='approved'`, receipt.RecordedAt, s.principal.TenantID, s.outlet, s.hospital, old.ID, old.Version)
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return errStaffBindingConflict
	}
	version := old.Version + 1
	receipt.ReplacedBindingID, receipt.ReplacedBindingVersion = &old.ID, &version
	previous := s
	previous.binding = old.ID
	return auditStaffBinding(tx, previous, actor, "end", &receipt.ID, receipt.RecordedAt, map[string]any{"status": "ended", "version": version, "related_binding_id": s.binding, "related_binding_version": proposal.Version + 1})
}

func (h *staffDepartmentBindingHandlers) get(c *gin.Context) {
	s, ok := bindingRequestScope(c)
	if !ok {
		return
	}
	var result StaffDepartmentBindingView
	err := h.command(c, s, PermStaffBindingRead, false, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		observed, err := readStaffBinding(tx, s, false)
		if err != nil {
			return err
		}
		read := func(held context.Context, current bool) error {
			return h.db.WithTenantCommand(held, s.principal.TenantID, func(tx *gorm.DB) error {
				if err := h.outlet(tx, s, false); err != nil {
					return err
				}
				row, err := readStaffBinding(tx, s, false)
				if err != nil {
					return err
				}
				state := "not_applicable"
				if row.Status == "approved" {
					state = "changed"
					if current {
						state = "current"
					}
				}
				var at time.Time
				if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
					return err
				}
				if err := auditStaffBinding(tx, s, actor, "read", nil, at, map[string]any{"version": row.Version, "source_state": state}); err != nil {
					return err
				}
				result = StaffDepartmentBindingView{Binding: row, SourceState: state}
				return nil
			})
		}
		err = h.departments.WithCurrentDepartment(ctx, authz.DepartmentReference{TenantID: s.principal.TenantID, HospitalID: s.hospital, DepartmentID: observed.DepartmentID, DepartmentRevision: observed.DepartmentRevisionID}, func(held context.Context) error { return read(held, true) })
		if errors.Is(err, authz.ErrDepartmentReferenceChanged) || errors.Is(err, authz.ErrDepartmentReferenceHidden) {
			return read(ctx, false)
		}
		return err
	})
	if err != nil {
		staffBindingError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *staffDepartmentBindingHandlers) receipt(c *gin.Context) {
	s, ok := bindingRequestScope(c)
	if !ok {
		return
	}
	id, valid := registryID(c, "command")
	if !valid {
		return
	}
	var result StaffDepartmentBindingCommandReceipt
	err := h.command(c, s, PermStaffBindingRead, false, func(_ context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		if err := h.outlet(tx, s, false); err != nil {
			return err
		}
		receipt, found, err := readStaffBindingReceipt(tx, s, id, "")
		if err != nil {
			return err
		}
		if !found {
			return gorm.ErrRecordNotFound
		}
		if err := auditStaffBindingReceiptRead(tx, s, actor, receipt); err != nil {
			return err
		}
		result = receipt
		return nil
	})
	if err != nil {
		staffBindingError(c, err)
		return
	}
	respond.OK(c, result)
}
