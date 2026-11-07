package supply

import (
	"time"

	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Protocol integration tests exercise the real owner store without adding a runtime API.
var TestRelationshipLock = relationshipLock
var TestLoadTradeRelationship = loadTradeRelationship
var TestSaveTradeRelationship = saveTradeRelationship
var TestEnqueueRelationshipExchange = enqueueRelationshipExchange

const TestRelationshipExchangeSubject = relationshipExchangeSubject

func RelationshipProofFixtureForTest(db *tenantdb.DB) (suppliertrade.CurrentRelationshipGuard, map[string]suppliertrade.RelationshipAuthorization) {
	owner, other := &relationshipAuthority{db: db}, &relationshipAuthority{db: db}
	return owner, map[string]suppliertrade.RelationshipAuthorization{
		"missing":      nil,
		"typed nil":    (*verifiedRelationship)(nil),
		"other issuer": &verifiedRelationship{owner: other, verifiedAt: time.Now()},
		"expired":      &verifiedRelationship{owner: owner, verifiedAt: time.Now().Add(-6 * time.Second)},
		"future":       &verifiedRelationship{owner: owner, verifiedAt: time.Now().Add(time.Minute)},
	}
}

func RelationshipSnapshotForTest(local suppliertrade.Relationship) suppliertrade.RelationshipAuthorization {
	return &verifiedRelationship{local: local}
}

func RelationshipAuthorizationForTest(db *tenantdb.DB, local suppliertrade.Relationship, at time.Time) (suppliertrade.CurrentRelationshipGuard, suppliertrade.RelationshipAuthorization) {
	owner := &relationshipAuthority{db: db}
	return owner, &verifiedRelationship{owner: owner, local: local, verifiedAt: at, scope: suppliertrade.RelationshipScope{TenantID: local.OwnerTenantID.String(), ID: local.ID, TermsSha256: local.TermsSha256, Purpose: "retained_transaction"}}
}
