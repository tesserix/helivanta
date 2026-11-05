package pharmacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

var (
	errStaffBindingConflict            = errors.New("department binding command or source changed")
	errStaffBindingEvidenceUnavailable = errors.New("department binding evidence could not be recorded")
)

func bindingV7(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

func bindingReason(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 1 && utf8.RuneCountInString(value) <= 500 && strings.IndexFunc(value, unicode.IsControl) < 0
}

func decodeBindingInput(c *gin.Context, value any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("expected one binding command")
	}
	return nil
}

func bindingIntentHash(subject string, intent any) (string, error) {
	data, err := json.Marshal(struct {
		Subject string `json:"subject"`
		Intent  any    `json:"intent"`
	}{subject, intent})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (h *staffDepartmentBindingHandlers) outlet(tx *gorm.DB, s staffBindingScope, writing bool) error {
	lock := `SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`
	if writing {
		lock = `SELECT pg_advisory_xact_lock(hashtextextended(?,0))`
	}
	if err := tx.Exec(lock, s.principal.TenantID+":pharmacy-staff-bindings:"+s.outlet.String()).Error; err != nil {
		return err
	}
	level, err := outletAccess(tx, s.principal, s.outlet, s.pharmacy, true)
	if err != nil {
		return err
	}
	if writing && level != "editor" {
		return gorm.ErrRecordNotFound
	}
	var organization struct{ OwnershipType string }
	if err := tx.Table("pharmacy_organizations").Select("ownership_type").Where("tenant_id=? AND id=?", s.principal.TenantID, s.pharmacy).Take(&organization).Error; err != nil {
		return err
	}
	if organization.OwnershipType != "hospital_owned" {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func readStaffBinding(tx *gorm.DB, s staffBindingScope, lock bool) (StaffDepartmentBinding, error) {
	var row StaffDepartmentBinding
	query := `SELECT b.*,o.pharmacy_id FROM pharmacy_staff_department_bindings b JOIN pharmacy_outlets o ON o.tenant_id=b.tenant_id AND o.id=b.outlet_id WHERE b.tenant_id=? AND b.outlet_id=? AND b.hospital_id=? AND b.id=? AND o.pharmacy_id=?`
	if lock {
		query += ` FOR UPDATE OF b`
	}
	result := tx.Raw(query, s.principal.TenantID, s.outlet, s.hospital, s.binding, s.pharmacy).Scan(&row)
	if result.Error != nil {
		return row, result.Error
	}
	if result.RowsAffected != 1 {
		return row, gorm.ErrRecordNotFound
	}
	return row, nil
}

func readStaffBindingReceipt(tx *gorm.DB, s staffBindingScope, id uuid.UUID, hash string) (StaffDepartmentBindingCommandReceipt, bool, error) {
	var stored struct {
		InputHash string
		Receipt   []byte
	}
	var receipt StaffDepartmentBindingCommandReceipt
	err := tx.Table("pharmacy_staff_binding_commands").Select("input_hash,receipt").Where("tenant_id=? AND outlet_id=? AND hospital_id=? AND binding_id=? AND id=?", s.principal.TenantID, s.outlet, s.hospital, s.binding, id).Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, false, nil
	}
	if err != nil {
		return receipt, false, err
	}
	if hash != "" && stored.InputHash != hash {
		return receipt, true, errStaffBindingConflict
	}
	if err := json.Unmarshal(stored.Receipt, &receipt); err != nil {
		return receipt, true, err
	}
	if receipt.ID != id || receipt.BindingID != s.binding || receipt.Binding.ID != s.binding || receipt.Binding.OutletID != s.outlet || receipt.Binding.HospitalID != s.hospital || receipt.Binding.PharmacyID != s.pharmacy {
		return receipt, true, authz.ErrScopedAuthorizationUnavailable
	}
	return receipt, true, nil
}

func (h *staffDepartmentBindingHandlers) record(tx *gorm.DB, s staffBindingScope, actor authz.StaffActorReference, hash string, receipt *StaffDepartmentBindingCommandReceipt) error {
	if h.bus == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	observations, err := staffBindingAuthorizationObservations(tx, s)
	if err != nil {
		return err
	}
	permission := PermStaffBindingReview
	if receipt.Operation == "propose" || receipt.Operation == "withdraw" {
		permission = PermStaffBindingPrepare
	}
	if len(observations) != 2 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	for _, observation := range observations {
		if observation.Permission != string(permission) && observation.Permission != string(authz.StaffActorUse) {
			return authz.ErrScopedAuthorizationUnavailable
		}
	}
	receipt.Authorizations = observations
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	source, err := json.Marshal(map[string]any{"actor": actor, "authorizations": observations})
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO pharmacy_staff_binding_commands(tenant_id,outlet_id,hospital_id,binding_id,id,operation,expected_version,input_hash,actor_subject,actor_staff_id,actor_source,reason,recorded_at,receipt) VALUES(?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?::jsonb)`, s.principal.TenantID, s.outlet, s.hospital, s.binding, receipt.ID, receipt.Operation, receipt.ExpectedVersion, hash, receipt.ActorSubject, receipt.ActorStaffID, string(source), receipt.Reason, receipt.RecordedAt, string(encoded)).Error; err != nil {
		return errors.Join(errStaffBindingEvidenceUnavailable, err)
	}
	if err := auditStaffBinding(tx, s, actor, receipt.Operation, &receipt.ID, receipt.RecordedAt, map[string]any{"version": receipt.Binding.Version, "status": receipt.Binding.Status, "related_binding_id": receipt.ReplacedBindingID, "related_binding_version": receipt.ReplacedBindingVersion}); err != nil {
		return err
	}
	data, err := json.Marshal(pharmacycontract.StaffDepartmentBindingChangedData{PharmacyID: s.pharmacy, OutletID: s.outlet, HospitalID: s.hospital, BindingID: s.binding, CommandID: receipt.ID, Version: receipt.Binding.Version, Status: receipt.Binding.Status, ReplacedBindingID: receipt.ReplacedBindingID, ReplacedBindingVersion: receipt.ReplacedBindingVersion})
	if err != nil {
		return err
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err := h.bus.Publish(tx, pharmacycontract.SubjectStaffDepartmentBindingChanged, events.Event{ID: eventID.String(), Type: pharmacycontract.SubjectStaffDepartmentBindingChanged, Version: 1, TenantID: s.principal.TenantID, OccurredAt: receipt.RecordedAt, Data: data}); err != nil {
		return errors.Join(errStaffBindingEvidenceUnavailable, err)
	}
	return nil
}

func auditStaffBinding(tx *gorm.DB, s staffBindingScope, actor authz.StaffActorReference, action string, command *uuid.UUID, at time.Time, changes map[string]any) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	observations, err := staffBindingAuthorizationObservations(tx, s)
	if err != nil {
		return err
	}
	changes["authorizations"] = observations
	changes["actor_staff_id"] = actor.StaffID
	changes["actor_link_id"] = actor.LinkID
	changes["actor_generation_id"] = actor.GenerationID
	data, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	var binding *uuid.UUID
	if s.binding != uuid.Nil {
		binding = &s.binding
	}
	if err := tx.Exec(`INSERT INTO pharmacy_staff_binding_audit(tenant_id,outlet_id,hospital_id,id,binding_id,actor_subject,action,command_id,recorded_at,changes) VALUES(?,?,?,?,?,?,?,?,?,?::jsonb)`, s.principal.TenantID, s.outlet, s.hospital, id, binding, s.principal.Subject, action, command, at, string(data)).Error; err != nil {
		return errors.Join(errStaffBindingEvidenceUnavailable, err)
	}
	return nil
}

func auditStaffBindingReceiptRead(tx *gorm.DB, s staffBindingScope, actor authz.StaffActorReference, receipt StaffDepartmentBindingCommandReceipt) error {
	var at time.Time
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
		return err
	}
	return auditStaffBinding(tx, s, actor, "read", &receipt.ID, at, map[string]any{"receipt_version": receipt.Binding.Version})
}

func (h *staffDepartmentBindingHandlers) put(c *gin.Context) {
	s, ok := bindingRequestScope(c)
	if !ok {
		return
	}
	var in StaffDepartmentBindingProposalInput
	if err := decodeBindingInput(c, &in); err != nil {
		respond.Error(c, 400, "invalid_request", "Provide one department binding proposal.")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !bindingV7(in.DepartmentID) || !bindingV7(in.DepartmentRevisionID) || !bindingReason(in.Reason) || (in.ReplacesBindingID == nil) != (in.ReplacesBindingVersion == nil) || in.ReplacesBindingID != nil && (!bindingV7(*in.ReplacesBindingID) || *in.ReplacesBindingID == s.binding || *in.ReplacesBindingVersion < 1 || *in.ReplacesBindingVersion > 9007199254740990) {
		respond.Error(c, 400, "invalid_request", "Choose a canonical department, exact replacement basis and reason.")
		return
	}
	hash, err := bindingIntentHash(s.principal.Subject, in)
	if err != nil {
		staffBindingError(c, err)
		return
	}
	var result StaffDepartmentBindingCommandReceipt
	err = h.command(c, s, PermStaffBindingPrepare, true, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		ref := authz.DepartmentReference{TenantID: s.principal.TenantID, HospitalID: s.hospital, DepartmentID: in.DepartmentID, DepartmentRevision: in.DepartmentRevisionID}
		return h.departments.WithCurrentDepartment(ctx, ref, func(held context.Context) error {
			return h.db.WithTenantCommand(held, s.principal.TenantID, func(tx *gorm.DB) error {
				if err := h.outlet(tx, s, true); err != nil {
					return err
				}
				receipt, found, err := readStaffBindingReceipt(tx, s, s.binding, hash)
				if err != nil {
					return err
				}
				if found {
					result = receipt
					return auditStaffBindingReceiptRead(tx, s, actor, receipt)
				}
				var at time.Time
				if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&at).Error; err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,id,department_id,department_revision_id,replaces_binding_id,replaces_binding_version,reason,status,version,proposed_by_subject,proposer_staff_id,created_at) VALUES(@tenant,@outlet,@hospital,@id,@department,@revision,@replacement,@replacement_version,@reason,'pending',1,@subject,@staff,@at)`, map[string]any{"tenant": s.principal.TenantID, "outlet": s.outlet, "hospital": s.hospital, "id": s.binding, "department": in.DepartmentID, "revision": in.DepartmentRevisionID, "replacement": in.ReplacesBindingID, "replacement_version": in.ReplacesBindingVersion, "reason": in.Reason, "subject": s.principal.Subject, "staff": actor.StaffID, "at": at}).Error; err != nil {
					return err
				}
				row, err := readStaffBinding(tx, s, false)
				if err != nil {
					return err
				}
				result = StaffDepartmentBindingCommandReceipt{ID: s.binding, BindingID: s.binding, Operation: "propose", ExpectedVersion: 0, Reason: in.Reason, ActorSubject: s.principal.Subject, ActorStaffID: actor.StaffID, RecordedAt: at, Binding: row}
				return h.record(tx, s, actor, hash, &result)
			})
		})
	})
	if err != nil {
		staffBindingError(c, err)
		return
	}
	respond.OK(c, result)
}
