package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

const supplierGrantExchangeSubject = iamcontract.SubjectSupplierGrantExchange

func supplierGrantLock(tx *gorm.DB, tenant string, id uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, tenant+":supplier-grant:"+id.String()).Error
}
func supplierGrantFrame(tx *gorm.DB, mode string, hospital uuid.UUID) error {
	value := ""
	if hospital != uuid.Nil {
		value = hospital.String()
	}
	return tx.Exec(`SELECT set_config('app.iam_supplier_access',?,true),set_config('app.iam_supplier_hospital',?,true)`, mode, value).Error
}
func loadSupplierGrant(tx *gorm.DB, tenant string, id uuid.UUID) (supplieraccess.SupplierGrant, error) {
	var row struct{ Document []byte }
	var grant supplieraccess.SupplierGrant
	if err := tx.Table("iam_supplier_grants").Select("document").Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error; err != nil {
		return grant, err
	}
	if suppliertrade.DecodeObject(row.Document, &grant) != nil || supplieraccess.ValidateRecord(grant) != nil || grant.OwnerTenantID.String() != tenant || grant.ID != id {
		return supplieraccess.SupplierGrant{}, supplieraccess.ErrInvalid
	}
	return grant, nil
}
func saveSupplierGrant(tx *gorm.DB, grant supplieraccess.SupplierGrant, actor string, eventID *uuid.UUID, create bool) error {
	if supplieraccess.ValidateRecord(grant) != nil {
		return supplieraccess.ErrInvalid
	}
	raw, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	if create {
		err = tx.Exec(`INSERT INTO iam_supplier_grants(tenant_id,id,origin_tenant_id,origin_record_id,hospital_id,relationship_id,document,input_hash,version,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?,?,?)`, grant.OwnerTenantID, grant.ID, grant.Scope.SourceTenantID, grant.ID, grant.Scope.HospitalID, grant.Input.RelationshipID, string(raw), grant.InputSha256, grant.Version, grant.CreatedAt, grant.UpdatedAt).Error
	} else {
		result := tx.Exec(`UPDATE iam_supplier_grants SET document=?::jsonb,version=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, string(raw), grant.Version, grant.UpdatedAt, grant.OwnerTenantID, grant.ID, grant.Version-1)
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = supplieraccess.ErrConflict
		}
	}
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO iam_supplier_grant_history(tenant_id,origin_tenant_id,origin_record_id,grant_id,hospital_id,version,document,actor_subject,source_event_id,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?,?,?)`, grant.OwnerTenantID, grant.Scope.SourceTenantID, grant.ID, grant.ID, grant.Scope.HospitalID, grant.Version, string(raw), actor, eventID, grant.UpdatedAt, grant.UpdatedAt).Error
}
func enqueueSupplierGrant(tx *gorm.DB, exchange *supplieraccess.SupplierGrantExchange) error {
	if exchange == nil {
		return nil
	}
	target := exchange.Scope.SourceTenantID
	if exchange.SenderTenantID == target {
		target = exchange.Scope.RecipientTenantID
	}
	raw, err := json.Marshal(exchange)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, supplierGrantExchangeSubject, events.Event{ID: id.String(), Type: supplierGrantExchangeSubject, Version: 1, OccurredAt: time.Now().UTC(), TenantID: exchange.SenderTenantID.String(), DestinationTenantID: target.String(), Data: raw})
}
func supplierGrantConsumer() events.Consumer {
	return events.Consumer{Subject: supplierGrantExchangeSubject, Name: "iam-supplier-grant-exchange", Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var input supplieraccess.SupplierGrantExchange
		origin, oe := uuid.Parse(event.TenantID)
		destination, de := uuid.Parse(event.DestinationTenantID)
		eventID, ie := uuid.Parse(event.ID)
		if oe != nil || de != nil || ie != nil || event.Version != 1 || event.Type != supplierGrantExchangeSubject || suppliertrade.DecodeObject(event.Data, &input) != nil || input.SenderTenantID != origin {
			return supplieraccess.ErrInvalid
		}
		if err := supplierGrantFrame(tx, "observe", input.Scope.HospitalID); err != nil {
			return err
		}
		if err := supplierGrantLock(tx, destination.String(), input.GrantID); err != nil {
			return err
		}
		existing, err := loadSupplierGrant(tx, destination.String(), input.GrantID)
		var previous *supplieraccess.SupplierGrant
		if err == nil {
			previous = &existing
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		next, reply, err := supplieraccess.Apply(previous, input, destination, time.Now())
		if err != nil {
			return err
		}
		if previous != nil && next.Version == previous.Version {
			return nil
		}
		if err = saveSupplierGrant(tx, next, "system:iam-supplier-grants", &eventID, previous == nil); err != nil {
			return err
		}
		return enqueueSupplierGrant(tx, reply)
	}}
}
func supplierCommandHash(actor string, input any) (string, error) {
	raw, err := json.Marshal(struct {
		Actor string `json:"actor"`
		Input any    `json:"input"`
	}{actor, input})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
