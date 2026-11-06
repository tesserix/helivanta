package iam

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func partnerGroupCommandID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("command"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param("command") {
		respond.NotFound(c, "partner group command")
		return uuid.Nil, false
	}
	return id, true
}

func partnerGroupCommandHash(subject string, group uuid.UUID, in PartnerGroupCommandInput) (string, error) {
	data, err := json.Marshal(struct {
		Subject string
		Group   uuid.UUID
		Input   PartnerGroupCommandInput
	}{subject, group, in})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readPartnerGroupCommand(tx *gorm.DB, tenant string, group, id uuid.UUID, hash string) (PartnerGroupCommandReceipt, error) {
	var row struct {
		InputHash string
		Receipt   []byte
	}
	var receipt PartnerGroupCommandReceipt
	if err := tx.Table("iam_partner_group_commands").Select("input_hash,receipt").Where("tenant_id=? AND group_id=? AND id=?", tenant, group, id).Take(&row).Error; err != nil {
		return receipt, err
	}
	if hash != "" && row.InputHash != hash {
		return receipt, errPartnerGroupConflict
	}
	if err := json.Unmarshal(row.Receipt, &receipt); err != nil {
		return receipt, err
	}
	if receipt.ID != id || receipt.GroupID != group || receipt.Group.ID != group || receipt.Group.Version != receipt.ResultVersion || receipt.ResultVersion != receipt.ExpectedVersion+1 || receipt.Group.ExternalAccessEnabled {
		return PartnerGroupCommandReceipt{}, errors.New("partner group command evidence is inconsistent")
	}
	return receipt, nil
}

func partnerGroupCommandState(group PartnerGroup, p authn.Principal, in PartnerGroupCommandInput) (string, error) {
	if group.Version != in.ExpectedVersion {
		return "", errPartnerGroupConflict
	}
	switch in.Operation {
	case "approve", "reject":
		if p.Subject == group.ProposedBy {
			return "", errAssignmentDenied
		}
		if group.Status != "pending_review" || !group.ExpiresAt.After(time.Now()) {
			return "", errPartnerGroupConflict
		}
		if in.Operation == "approve" {
			return "approved", nil
		}
		return "rejected", nil
	case "withdraw":
		if p.Subject != group.ProposedBy {
			return "", errAssignmentDenied
		}
		if group.Status != "pending_scope" && group.Status != "pending_review" {
			return "", errPartnerGroupConflict
		}
		return "withdrawn", nil
	case "suspend":
		if group.Status != "approved" {
			return "", errPartnerGroupConflict
		}
		return "suspended", nil
	}
	return "", errPartnerGroupConflict
}

func (h *partnerGroupHandlers) putCommand(c *gin.Context) {
	p, groupID, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	commandID, ok := partnerGroupCommandID(c)
	if !ok {
		return
	}
	var in PartnerGroupCommandInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Provide one bounded group decision.")
		return
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) || !partnerGroupText(in.Reason, 500) || in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || (in.Operation != "approve" && in.Operation != "reject" && in.Operation != "withdraw" && in.Operation != "suspend") {
		respond.Error(c, 400, "invalid_request", "Provide the decision, exact current group version and reason.")
		return
	}
	permission := PermAssignmentReview
	if in.Operation == "withdraw" || in.Operation == "suspend" {
		permission = PermAssignmentManage
	}
	permissions, _ := authz.PermissionsFrom(c)
	if !permissions.Has(permission) {
		respond.Forbidden(c, "This group decision is not permitted.")
		return
	}
	hash, err := partnerGroupCommandHash(p.Subject, groupID, in)
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var receipt PartnerGroupCommandReceipt
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','write',true),set_config('app.actor_subject',?,true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.Subject, p.TenantID+":partner-group:"+groupID.String()).Error; err != nil {
			return err
		}
		var err error
		receipt, err = readPartnerGroupCommand(tx, p.TenantID, groupID, commandID, hash)
		if err == nil {
			return partnerGroupReceiptAudit(tx, p, groupID, "partner_group_command_receipt_read")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		group, err := readPartnerGroup(tx, p.TenantID, groupID)
		if err != nil {
			return err
		}
		status, err := partnerGroupCommandState(group, p, in)
		if err != nil {
			return err
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
		if err != nil {
			return err
		}
		if epoch == 0 {
			return errRevocationActorNotAdmin
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		group.Status = status
		group.Version++
		group.UpdatedAt = now
		receipt = PartnerGroupCommandReceipt{ID: commandID, GroupID: groupID, Operation: in.Operation, ExpectedVersion: in.ExpectedVersion, ResultVersion: group.Version, ActorSubject: p.Subject, ActorMembershipEpoch: epoch, Reason: in.Reason, Group: group, CreatedAt: now}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_partner_group_commands(tenant_id,id,group_id,hospital_id,operation,expected_version,result_version,result_status,actor_subject,actor_membership_epoch,reason,input_hash,receipt,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?)`, p.TenantID, commandID, groupID, group.HospitalID, in.Operation, in.ExpectedVersion, group.Version, status, p.Subject, epoch, in.Reason, hash, string(encoded), now).Error; err != nil {
			return err
		}
		if err := partnerGroupReceiptAudit(tx, p, groupID, "partner_group_"+in.Operation); err != nil {
			return err
		}
		if h.bus == nil {
			return errors.New("partner group change publication unavailable")
		}
		payload, err := json.Marshal(iamcontract.PartnerGroupChangedData{GroupID: groupID, HospitalID: group.HospitalID, Version: group.Version, Status: status})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectPartnerGroupChanged, events.Event{Type: iamcontract.SubjectPartnerGroupChanged, Version: 1, TenantID: p.TenantID, Data: payload})
	})
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	respond.OK(c, receipt)
}

func (h *partnerGroupHandlers) getCommand(c *gin.Context) {
	p, groupID, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	id, ok := partnerGroupCommandID(c)
	if !ok {
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var receipt PartnerGroupCommandReceipt
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','read',true)`).Error; err != nil {
			return err
		}
		var err error
		receipt, err = readPartnerGroupCommand(tx, p.TenantID, groupID, id, "")
		if err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, groupID, "partner_group_command_receipt_read")
	})
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	respond.OK(c, receipt)
}
