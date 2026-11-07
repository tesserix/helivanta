package supply

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
	"time"
)

//go:embed trade_relationships.sql
var tradeRelationshipsSchema string

const relationshipExchangeSubject = supplycontract.SubjectRelationshipExchange

func relationshipLock(tx *gorm.DB, tenant, id uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, tenant.String()+":supplier-relationship:"+id.String()).Error
}
func loadTradeRelationship(tx *gorm.DB, tenant, id uuid.UUID) (suppliertrade.Relationship, error) {
	var body string
	err := tx.Raw(`SELECT body FROM supply_trade_relationships WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, id).Row().Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return suppliertrade.Relationship{}, gorm.ErrRecordNotFound
	}
	if err != nil {
		return suppliertrade.Relationship{}, err
	}
	var result suppliertrade.Relationship
	if err := suppliertrade.DecodeObject([]byte(body), &result); err != nil {
		return result, err
	}
	return result, nil
}
func saveTradeRelationship(tx *gorm.DB, r suppliertrade.Relationship, original *suppliertrade.SupplierRelationshipInput, actor string, eventID *uuid.UUID) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if r.Version == 1 {
		var input, receipt *string
		if original != nil {
			raw, err := json.Marshal(original)
			if err != nil {
				return err
			}
			inputText, receiptText := string(raw), string(body)
			input = &inputText
			receipt = &receiptText
		}
		if err := tx.Exec(`INSERT INTO supply_trade_relationships(tenant_id,origin_tenant_id,origin_record_id,id,body,proposal_input,proposal_receipt,version,created_at,updated_at)VALUES(?,?,?,?,?::jsonb,?::jsonb,?::jsonb,?,?,?)`, r.OwnerTenantID, r.Terms.Seller.TenantID, r.ID, r.ID, string(body), input, receipt, r.Version, r.CreatedAt, r.UpdatedAt).Error; err != nil {
			return err
		}
	} else {
		result := tx.Exec(`UPDATE supply_trade_relationships SET body=?::jsonb,version=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, string(body), r.Version, r.UpdatedAt, r.OwnerTenantID, r.ID, r.Version-1)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return suppliertrade.ErrConflict
		}
	}
	return tx.Exec(`INSERT INTO supply_trade_relationship_history(tenant_id,origin_tenant_id,origin_record_id,relationship_id,version,body,actor_subject,source_event_id,created_at)VALUES(?,?,?,?,?,?::jsonb,?,?,?)`, r.OwnerTenantID, r.Terms.Seller.TenantID, r.ID, r.ID, r.Version, string(body), actor, eventID, r.UpdatedAt).Error
}
func enqueueRelationshipExchange(tx *gorm.DB, e *suppliertrade.RelationshipExchange) error {
	if e == nil {
		return nil
	}
	destination := e.Terms.Buyer.TenantID
	if e.SourceTenantID == destination {
		destination = e.Terms.Seller.TenantID
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, relationshipExchangeSubject, events.Event{ID: id.String(), Type: relationshipExchangeSubject, Version: 1, OccurredAt: time.Now().UTC(), TenantID: e.SourceTenantID.String(), DestinationTenantID: destination.String(), Data: raw})
}
func tradeRelationshipConsumer() events.Consumer {
	return events.Consumer{Name: "supply-relationship-exchange", Subject: relationshipExchangeSubject, Handle: func(ctx context.Context, tx *gorm.DB, event events.Event) error {
		var input suppliertrade.RelationshipExchange
		source, sourceErr := uuid.Parse(event.TenantID)
		destination, destErr := uuid.Parse(event.DestinationTenantID)
		eventID, eventErr := uuid.Parse(event.ID)
		if sourceErr != nil || destErr != nil || eventErr != nil || event.Type != relationshipExchangeSubject || event.Version != 1 || suppliertrade.DecodeObject(event.Data, &input) != nil || input.SourceTenantID != source {
			return errors.New("invalid relationship exchange provenance")
		}
		if err := relationshipLock(tx, destination, input.RelationshipID); err != nil {
			return err
		}
		previous, err := loadTradeRelationship(tx, destination, input.RelationshipID)
		var existing *suppliertrade.Relationship
		if err == nil {
			existing = &previous
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		next, reply, err := suppliertrade.ApplyRelationshipExchange(existing, input, destination, time.Now())
		if err != nil {
			return err
		}
		if existing != nil && next.Version == previous.Version {
			return nil
		}
		if err := saveTradeRelationship(tx, next, nil, "system:supplier-relationship", &eventID); err != nil {
			return err
		}
		return enqueueRelationshipExchange(tx, reply)
	}}
}
