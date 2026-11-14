package supply

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"sync/atomic"
	"time"
)

type relationshipAuthority struct {
	db       *tenantdb.DB
	sources  suppliertrade.CurrentEndpointGuard
	peer     suppliertrade.SourceChecker
	identity *organizationidentity.Client
}
type verifiedRelationship struct {
	owner      *relationshipAuthority
	scope      suppliertrade.RelationshipScope
	local      suppliertrade.Relationship
	peer       suppliertrade.RelationshipCheckResult
	verifiedAt time.Time
	consumed   atomic.Bool
}

func (p *verifiedRelationship) Snapshot() suppliertrade.Relationship {
	// Do not let a consumer mutate pointer fields retained by an opaque authorization.
	raw, _ := json.Marshal(p.local)
	var result suppliertrade.Relationship
	_ = json.Unmarshal(raw, &result)
	return result
}
func NewRelationshipAuthority(db *tenantdb.DB, sources suppliertrade.CurrentEndpointGuard, peer suppliertrade.SourceChecker, identity *organizationidentity.Client) suppliertrade.CurrentRelationshipGuard {
	return &relationshipAuthority{db: db, sources: sources, peer: peer, identity: identity}
}
func (g *relationshipAuthority) VerifyRelationship(ctx context.Context, scope suppliertrade.RelationshipScope) (suppliertrade.RelationshipAuthorization, error) {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || scope.ID.Version() != 7 || (scope.Purpose != "new_business" && scope.Purpose != "retained_transaction") {
		return nil, suppliertrade.ErrInvalid
	}
	peer, ok := g.peer.(suppliertrade.RelationshipChecker)
	if g.db == nil || g.sources == nil || g.identity == nil || g.peer == nil || !ok || g.db.GuardedCommandActive(ctx) {
		return nil, suppliertrade.ErrUnavailable
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var local suppliertrade.Relationship
	err = g.db.WithTenant(ctx, scope.TenantID, func(tx *gorm.DB) error {
		var err error
		local, err = loadTradeRelationship(tx, tenant, scope.ID)
		if err != nil {
			return err
		}
		if scope.TermsSha256 != "" && local.TermsSha256 != scope.TermsSha256 {
			return suppliertrade.ErrIneligible
		}
		if scope.Purpose == "new_business" && !suppliertrade.RelationshipCurrent(local, time.Now()) {
			return suppliertrade.ErrIneligible
		}
		if scope.Purpose == "retained_transaction" {
			var prior bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM supply_trade_relationship_history WHERE tenant_id=? AND relationship_id=? AND body->>'status'='active')`, tenant, scope.ID).Scan(&prior).Error; err != nil {
				return err
			}
			if !prior || local.Status == "rejected" {
				return suppliertrade.ErrIneligible
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, suppliertrade.ErrIneligible
	}
	if err != nil {
		return nil, err
	}
	// Each owner is queried anew. These are bounded observations, not cached positive eligibility.
	for _, tenant := range []uuid.UUID{local.Terms.Seller.TenantID, local.Terms.Buyer.TenantID} {
		if _, err := g.identity.Verify(ctx, tenant); err != nil {
			if errors.Is(err, organizationidentity.ErrIneligible) {
				return nil, suppliertrade.ErrIneligible
			}
			return nil, suppliertrade.ErrUnavailable
		}
	}
	remote := local.Terms.Buyer
	if local.Side == "buyer" {
		remote = local.Terms.Seller
	}
	counterpart, err := peer.CheckRelationship(ctx, suppliertrade.RelationshipCheckInput{OwnerTenantID: remote.TenantID, PeerTenantID: tenant, RelationshipID: scope.ID, TermsSha256: local.TermsSha256, Purpose: scope.Purpose, ExpectedAuthorityEpoch: local.RemoteEpoch})
	if err != nil {
		return nil, err
	}
	if scope.Purpose == "new_business" {
		if counterpart.RemoteEpoch != local.AuthorityEpoch {
			return nil, suppliertrade.ErrIneligible
		}
		if _, err := g.peer.Check(ctx, suppliertrade.SourceCheckInput{Endpoint: remote, Category: local.Terms.Category, Operation: "order"}); err != nil {
			return nil, err
		}
	}
	if time.Since(started) > 5*time.Second {
		return nil, suppliertrade.ErrUnavailable
	}
	return &verifiedRelationship{owner: g, scope: scope, local: local, peer: counterpart, verifiedAt: started}, nil
}
func (g *relationshipAuthority) WithVerifiedRelationship(ctx context.Context, proof suppliertrade.RelationshipAuthorization, action func(context.Context, suppliertrade.Relationship) error) error {
	token, ok := proof.(*verifiedRelationship)
	if !ok || token == nil || token.owner != g || g.db == nil || action == nil || !g.db.GuardedCommandActive(ctx) || time.Since(token.verifiedAt) < 0 || time.Since(token.verifiedAt) > 5*time.Second {
		return suppliertrade.ErrUnavailable
	}
	if !token.consumed.CompareAndSwap(false, true) {
		return suppliertrade.ErrUnavailable
	}
	if err := g.db.ConstrainGuardedCommand(ctx, token.scope.TenantID, token.verifiedAt.Add(5*time.Second)); err != nil {
		return suppliertrade.ErrUnavailable
	}
	return g.db.WithTenantCommand(ctx, token.scope.TenantID, func(tx *gorm.DB) error {
		if err := relationshipLock(tx, token.local.OwnerTenantID, token.local.ID); err != nil {
			return err
		}
		current, err := loadTradeRelationship(tx, token.local.OwnerTenantID, token.local.ID)
		if err != nil {
			return err
		}
		if current.Version != token.local.Version || current.TermsSha256 != token.local.TermsSha256 || current.AuthorityEpoch != token.local.AuthorityEpoch || current.RemoteEpoch != token.local.RemoteEpoch {
			return suppliertrade.ErrConflict
		}
		if token.scope.Purpose == "new_business" {
			if !suppliertrade.RelationshipCurrent(current, time.Now()) || token.peer.AuthorityEpoch != current.RemoteEpoch || token.peer.RemoteEpoch != current.AuthorityEpoch {
				return suppliertrade.ErrIneligible
			}
			return g.sources.WithCurrentTradingEndpoint(ctx, localRelationshipEndpoint(current), current.Terms.Category, "order", func(ctx context.Context, _ suppliertrade.EndpointReference) error {
				return action(ctx, token.Snapshot())
			})
		}
		if current.Status == "rejected" {
			return suppliertrade.ErrIneligible
		}
		return action(ctx, token.Snapshot())
	})
}
