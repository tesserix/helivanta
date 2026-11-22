package globalapi

import (
	"context"
	"testing"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

type observedCompoundPartnerScopes struct {
	authz.PartnerScopeAuthorizer
	t *testing.T
}

type observedCompoundRelationships struct {
	suppliertrade.CurrentRelationshipGuard
	t *testing.T
}

func (a observedCompoundRelationships) VerifyRelationship(ctx context.Context, scope suppliertrade.RelationshipScope) (suppliertrade.RelationshipAuthorization, error) {
	proof, err := a.CurrentRelationshipGuard.VerifyRelationship(ctx, scope)
	a.t.Logf("compound relationship verification: proof_present=%t error=%v", proof != nil, err)
	return proof, err
}

func (a observedCompoundRelationships) WithVerifiedRelationship(ctx context.Context, proof suppliertrade.RelationshipAuthorization, action func(context.Context, suppliertrade.Relationship) error) error {
	if action == nil {
		return a.CurrentRelationshipGuard.WithVerifiedRelationship(ctx, proof, nil)
	}
	entered := false
	err := a.CurrentRelationshipGuard.WithVerifiedRelationship(ctx, proof, func(current context.Context, relationship suppliertrade.Relationship) error {
		entered = true
		return action(current, relationship)
	})
	if err != nil {
		a.t.Logf("compound relationship use: consumer_entered=%t error=%v", entered, err)
	}
	return err
}

func (a observedCompoundPartnerScopes) WithPartnerScope(ctx context.Context, request authz.PartnerScopeRequest, action func(context.Context, authz.PartnerScopeGrant) error) error {
	if action == nil {
		return a.PartnerScopeAuthorizer.WithPartnerScope(ctx, request, nil)
	}
	entered := false
	err := a.PartnerScopeAuthorizer.WithPartnerScope(ctx, request, func(current context.Context, grant authz.PartnerScopeGrant) error {
		entered = true
		return action(current, grant)
	})
	if err != nil {
		a.t.Logf("compound partner scope: consumer_entered=%t error=%v", entered, err)
	}
	return err
}
