package authz

import (
	"encoding/json"
	"testing"

	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
)

func TestBootRepairsChangedPermissionRules(t *testing.T) {
	ctx := t.Context()
	endpoint, store := testinfra.StartOpenFGA(t), testinfra.IsolationKey(t)
	client, err := NewClient(ctx, endpoint, store)
	require.NoError(t, err)
	var altered fgaclient.ClientWriteAuthorizationModelRequest
	require.NoError(t, json.Unmarshal([]byte(modelJSON), &altered))
	for i := range altered.TypeDefinitions {
		definition := &altered.TypeDefinitions[i]
		if definition.Type != "perm" {
			continue
		}
		direct := openfga.NewUserset()
		direct.SetThis(map[string]interface{}{})
		(*definition.Relations)["can_do"] = *direct
		metadata := openfga.NewRelationMetadata()
		metadata.SetDirectlyRelatedUserTypes([]openfga.RelationReference{*openfga.NewRelationReference("user")})
		(*definition.Metadata.Relations)["can_do"] = *metadata
	}
	_, err = client.api.WriteAuthorizationModel(ctx).Body(altered).Execute()
	require.NoError(t, err)
	const tenant = "11111111-1111-1111-1111-111111111111"
	const permission Permission = "workforce.payroll.view"
	require.NoError(t, client.write(ctx, "user:direct-reader", "can_do", PermObject(tenant, permission)))
	require.NoError(t, client.GrantPermission(ctx, tenant, permission, RoleTenantAdmin))
	require.NoError(t, client.GrantRole(ctx, tenant, "approved-reader", RoleTenantAdmin))
	before, err := client.Resolve(ctx, "direct-reader", tenant)
	require.NoError(t, err)
	require.True(t, before.Has(permission), "fixture must expose the obsolete direct grant")

	repaired, err := NewClient(ctx, endpoint, store)
	require.NoError(t, err)
	after, err := repaired.Resolve(ctx, "direct-reader", tenant)
	require.NoError(t, err)
	require.False(t, after.Has(permission), "boot must repair changed rules even when type and relation names match")
	approved, err := repaired.Resolve(ctx, "approved-reader", tenant)
	require.NoError(t, err)
	require.True(t, approved.Has(permission), "the declared role-based rule must work after repair")
	models, err := repaired.api.ReadAuthorizationModels(ctx).Execute()
	require.NoError(t, err)
	_, err = NewClient(ctx, endpoint, store)
	require.NoError(t, err)
	rebooted, err := repaired.api.ReadAuthorizationModels(ctx).Execute()
	require.NoError(t, err)
	require.Equal(t, models.GetAuthorizationModels()[0].GetId(), rebooted.GetAuthorizationModels()[0].GetId(), "unchanged boot must reuse the model")
}

func TestBootRemovesWildcardRoleAuthority(t *testing.T) {
	ctx := t.Context()
	endpoint, store := testinfra.StartOpenFGA(t), testinfra.IsolationKey(t)
	client, err := NewClient(ctx, endpoint, store)
	require.NoError(t, err)
	var altered fgaclient.ClientWriteAuthorizationModelRequest
	require.NoError(t, json.Unmarshal([]byte(modelJSON), &altered))
	for i := range altered.TypeDefinitions {
		definition := &altered.TypeDefinitions[i]
		if definition.Type != "role" {
			continue
		}
		metadata := (*definition.Metadata.Relations)["assignee"]
		wildcard := openfga.NewRelationReference("user")
		wildcard.SetWildcard(map[string]interface{}{})
		metadata.SetDirectlyRelatedUserTypes(append(metadata.GetDirectlyRelatedUserTypes(), *wildcard))
		(*definition.Metadata.Relations)["assignee"] = metadata
	}
	_, err = client.api.WriteAuthorizationModel(ctx).Body(altered).Execute()
	require.NoError(t, err)
	const tenant = "11111111-1111-1111-1111-111111111111"
	const permission Permission = "workforce.payroll.view"
	require.NoError(t, client.GrantPermission(ctx, tenant, permission, RoleTenantAdmin))
	require.NoError(t, client.write(ctx, "user:*", "assignee", RoleObject(tenant, RoleTenantAdmin)))
	before, err := client.Resolve(ctx, "unassigned-reader", tenant)
	require.NoError(t, err)
	require.True(t, before.Has(permission))
	repaired, err := NewClient(ctx, endpoint, store)
	require.NoError(t, err)
	after, err := repaired.Resolve(ctx, "unassigned-reader", tenant)
	require.NoError(t, err)
	require.False(t, after.Has(permission), "metadata-only changes must remove obsolete wildcard authority")
}

func TestModelComparisonPreservesConditionRules(t *testing.T) {
	var desired fgaclient.ClientWriteAuthorizationModelRequest
	require.NoError(t, json.Unmarshal([]byte(modelJSON), &desired))
	desired.SetConditions(map[string]openfga.Condition{"window": *openfga.NewCondition("window", "false")})
	existing := openfga.AuthorizationModel{SchemaVersion: desired.SchemaVersion, TypeDefinitions: desired.TypeDefinitions, Conditions: desired.Conditions}
	require.True(t, sameAuthorizationModel(existing, desired))
	existing.SetConditions(map[string]openfga.Condition{"window": *openfga.NewCondition("window", "true")})
	require.False(t, sameAuthorizationModel(existing, desired), "same condition name with a different expression is a different authorization model")
	existing.Conditions = desired.Conditions
	existing.SchemaVersion = "1.0"
	require.False(t, sameAuthorizationModel(existing, desired))
}

func TestModelComparisonRejectsDuplicateTypes(t *testing.T) {
	var desired fgaclient.ClientWriteAuthorizationModelRequest
	require.NoError(t, json.Unmarshal([]byte(modelJSON), &desired))
	existing := openfga.AuthorizationModel{SchemaVersion: desired.SchemaVersion, TypeDefinitions: desired.TypeDefinitions}
	desired.TypeDefinitions = append(desired.TypeDefinitions, desired.TypeDefinitions[0])
	require.False(t, sameAuthorizationModel(existing, desired), "an invalid duplicate declaration must reach server validation instead of being treated as an unchanged model")
}
