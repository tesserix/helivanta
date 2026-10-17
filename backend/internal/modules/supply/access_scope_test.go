package supply_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's public test interface
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func scopedAccessHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	return accessHarnessWithRoles(t, accessMembership{})
}
func accessBody(version int, subject, role string, premise any, from, expiry time.Time, status string) string {
	data, e := json.Marshal(map[string]any{"expected_version": version, "subject": subject, "role": role, "premise_id": premise, "valid_from": from.UTC().Format(time.RFC3339), "expires_at": expiry.UTC().Format(time.RFC3339), "reason": "TEST approved operational access", "status": status})
	if e != nil {
		panic(e)
	}
	return string(data)
}
func TestExplicitAccessGrantIsVersionedRetrySafeAndRevocable(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	body := accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active")
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"active"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", body).Body.String())
	require.Equal(t, 200, testutil.Do(r, "GET", root, "auditor", "").Code)
	require.Contains(t, testutil.Do(r, "GET", root+"/access", "auditor", "").Body.String(), `"auditor"`)
	denied := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "auditor", accessBody(0, "user-auditor", "administrator", nil, from, expiry, "active"))
	require.Equal(t, 404, denied.Code, denied.Body.String())
	duplicate := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", body)
	require.Equal(t, 409, duplicate.Code, duplicate.Body.String())
	revoked := accessBody(1, "user-auditor", "auditor", nil, from, expiry, "revoked")
	w = testutil.Do(r, "PUT", path, "owner", revoked)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", revoked).Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/access", "auditor", "").Code)
	history := testutil.Do(r, "GET", path+"/revisions?limit=1", "owner", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), `"version":2`)
	require.Contains(t, history.Body.String(), `"has_more":true`)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if e := tx.Table("outbox_events").Where("subject='helivanta.in.supply.party_access_changed.v1'").Count(&n).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, n)
		return nil
	}))
}
func TestLocationAndScheduledGrantsDoNotExposePrivatePartyProfile(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	location := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/premises/"+location, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	grant := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(r, "PUT", grant, "owner", accessBody(0, "user-manager", "warehouse", location, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, suffix := range []string{"", "/premises/" + location, "/licenses", "/revisions"} {
		denied := testutil.Do(r, "GET", root+suffix, "manager", "")
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	w = testutil.Do(r, "GET", root+"/access", "manager", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `{"roles":[],"operating_roles":["warehouse"]}`, w.Body.String())
	w = testutil.Do(r, "GET", "/v1/supply/parties", "manager", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"access_roles":[]`)
	require.Contains(t, w.Body.String(), `"operating_roles":["warehouse"]`)
	future := time.Now().Add(24 * time.Hour)
	w = testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-auditor", "auditor", nil, future, future.Add(time.Hour), "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"scheduled"`)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
	revokeOperatingGrant(t, r, grant)
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/access", "manager", "").Code)
}
func TestNewGrantRequiresTrustedMembershipAndActiveSamePartyLocation(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	for _, entry := range []struct {
		subject, role string
		premise       any
		code          int
	}{{"unknown-user", "auditor", nil, 404}, {"user-manager", "administrator", uuid.Must(uuid.NewV7()).String(), 400}, {"user-manager", "warehouse", uuid.Must(uuid.NewV7()).String(), 404}} {
		w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, entry.subject, entry.role, entry.premise, from, expiry, "active"))
		require.Equal(t, entry.code, w.Code, w.Body.String())
	}
}

func TestTemporaryAdministratorCannotMintPermanentOrLongerAuthority(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(3 * time.Hour)
	w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-manager", "administrator", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "PUT", root+"/access/user-auditor/administrator", "manager", "").Code)
	denied := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "manager", accessBody(0, "user-auditor", "auditor", nil, from, expiry.Add(time.Hour), "active"))
	require.Equal(t, 409, denied.Code, denied.Body.String())
	allowed := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "manager", accessBody(0, "user-auditor", "auditor", nil, from, expiry.Add(-time.Hour), "active"))
	require.Equal(t, 200, allowed.Code, allowed.Body.String())
}
func TestExpiredAccessIsDeniedByAPIAndDatabaseWithoutBackgroundJob(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	id := uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	w := testutil.Do(r, "PUT", root+"/grants/"+id, "owner", accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	partyID := strings.TrimPrefix(root, "/v1/supply/parties/")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.actor_subject','user-owner',true),set_config('app.supply_access_admin','on',true)`, partyID).Error; e != nil {
			return e
		}
		if e := tx.Exec(`UPDATE supply_access_grants SET expires_at=clock_timestamp()-interval '1 second',version=version+1 WHERE tenant_id=? AND party_id=? AND id=?`, testutil.TenantA, partyID, id).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO supply_access_grant_revisions(tenant_id,party_id,grant_id,id,version,snapshot,actor_subject) SELECT tenant_id,party_id,id,?,version,to_jsonb(supply_access_grants),'user-owner' FROM supply_access_grants WHERE tenant_id=? AND party_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, partyID, id).Error
	}))
	for _, suffix := range []string{"", "/access", "/revisions", "/premises", "/licenses"} {
		denied := testutil.Do(r, "GET", root+suffix, "auditor", "")
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	require.Contains(t, testutil.Do(r, "GET", root+"/grants/"+id, "owner", "").Body.String(), `"effective_status":"expired"`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-auditor',true)`, partyID).Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Table("supply_parties").Where("id=?", partyID).Count(&n).Error; e != nil {
			return e
		}
		require.Zero(t, n)
		return nil
	}))
}

func TestFiniteAdministratorCannotReplacePermanentRecoveryAnchor(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Minute)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-manager", "administrator", nil, from, from.Add(time.Hour), "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "DELETE", root+"/access/user-owner/administrator", "manager", "")
	require.Equal(t, 409, w.Code, "temporary authority must not leave the organisation without an administrator after expiry: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "permanent organisation administrator")
	require.Equal(t, 200, testutil.Do(r, "GET", root, "owner", "").Code)
}

func TestLastEffectiveAdministratorIsProtectedAcrossBothGrantSources(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-manager", "administrator", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-auditor/administrator", "owner", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", root+"/access/user-owner/administrator", "owner", "").Code)
	denied := testutil.Do(r, "DELETE", root+"/access/user-auditor/administrator", "manager", "")
	require.Equal(t, 409, denied.Code, denied.Body.String())
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", accessBody(1, "user-manager", "administrator", nil, from, expiry, "revoked")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "manager", accessBody(1, "user-manager", "administrator", nil, from, expiry, "revoked")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "manager", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", root, "auditor", "").Code)
}
func TestAccessOutboxFailureRollsBackGrantAndRevision(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_explicit_access_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_explicit_access_outbox_failure CHECK(subject<>'helivanta.in.supply.party_access_changed.v1') NOT VALID`}}))
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "test_explicit_access_outbox_failure")
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.actor_subject','user-owner',true),set_config('app.supply_access_admin','on',true)`, strings.TrimPrefix(root, "/v1/supply/parties/")).Error; e != nil {
			return e
		}
		for _, table := range []string{"supply_access_grants", "supply_access_grant_revisions"} {
			var n int64
			if e := tx.Table(table).Count(&n).Error; e != nil {
				return e
			}
			require.Zero(t, n)
		}
		return nil
	}))
}
func TestAccessConcurrentExactRetriesPublishOneRevision(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	body := accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active")
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		group.Go(func() { codes <- testutil.Do(r, "PUT", path, "owner", body).Code })
	}
	group.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 200, code)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.actor_subject','user-owner',true),set_config('app.supply_access_admin','on',true)`, strings.TrimPrefix(root, "/v1/supply/parties/")).Error; e != nil {
			return e
		}
		for _, table := range []string{"supply_access_grants", "supply_access_grant_revisions"} {
			var n int64
			if e := tx.Table(table).Count(&n).Error; e != nil {
				return e
			}
			require.EqualValues(t, 1, n)
		}
		return nil
	}))
}

type unavailableAccessMembership struct{}

func (unavailableAccessMembership) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return nil, errors.New("TEST membership service unavailable")
}
func TestAccessMembershipFailureDeniesWithoutWriting(t *testing.T) {
	permissions := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.party.access.manage", "supply.organization.manage"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": permissions}, Roles: unavailableAccessMembership{}, Modules: []platform.Module{supply.New()}})
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-auditor", "auditor", nil, from, from.Add(24*time.Hour), "active"))
	require.Equal(t, 503, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "TEST membership")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if e := tx.Table("supply_access_grants").Count(&n).Error; e != nil {
			return e
		}
		require.Zero(t, n)
		return nil
	}))
}

func TestAccessUpdatesRejectStaleVersionsAndIdentityChanges(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	changed := accessBody(1, "user-auditor", "auditor", nil, from, expiry.Add(time.Hour), "active")
	w = testutil.Do(r, "PUT", path, "owner", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", changed).Body.String())
	for _, body := range []string{accessBody(1, "user-auditor", "auditor", nil, from, expiry.Add(2*time.Hour), "active"), accessBody(2, "user-manager", "auditor", nil, from, expiry.Add(time.Hour), "active"), accessBody(99, "user-auditor", "auditor", nil, from, expiry.Add(time.Hour), "active")} {
		denied := testutil.Do(r, "PUT", path, "owner", body)
		require.Equal(t, 409, denied.Code, denied.Body.String())
	}
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", strings.TrimSuffix(changed, "}")+`,"unexpected":true}`).Code)
}

func TestGrantReasonsRemainPrivateAcrossOrganisationsAndAuthorityLookups(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	otherID := uuid.Must(uuid.NewV7()).String()
	otherBody := strings.ReplaceAll(strings.ReplaceAll(strings.Replace(partyBody(0, "TEST Separate Organisation"), "counterparty", "owned_organisation", 1), "TEST-DISTRIBUTOR", "TEST-OTHER"), "TEST-REG-001", "TEST-REG-OTHER")
	w = testutil.Do(r, "PUT", "/v1/supply/parties/"+otherID, "manager", otherBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, suffix := range []string{"/grants", "/access/grants"} {
		denied := testutil.Do(r, "GET", root+suffix, "manager", "")
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-manager',true),set_config('app.supply_authority_lookup','off',true)`, otherID).Error; e != nil {
			return e
		}
		var reasons []string
		if e := tx.Table("supply_access_grants").Pluck("reason", &reasons).Error; e != nil {
			return e
		}
		require.Empty(t, reasons, "another organisation's grant reason must not be visible")
		var allowed bool
		if e := tx.Raw(`SELECT supply_effective_role(?,?,?,ARRAY['auditor'],NULL)`, testutil.TenantA, strings.TrimPrefix(root, "/v1/supply/parties/"), "user-auditor").Scan(&allowed).Error; e != nil {
			return e
		}
		require.True(t, allowed)
		var flag string
		if e := tx.Raw(`SELECT current_setting('app.supply_authority_lookup',true)`).Scan(&flag).Error; e != nil {
			return e
		}
		require.Equal(t, "off", flag)
		if e := tx.Table("supply_access_grants").Pluck("reason", &reasons).Error; e != nil {
			return e
		}
		require.Empty(t, reasons, "authority lookup must restore metadata isolation")
		return nil
	}))
}

func TestAuthorityLookupRestoresMetadataBoundaryAfterDatabaseError(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	expiry := from.Add(24 * time.Hour)
	w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-auditor", "auditor", nil, from, expiry, "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_authority_failure", SQL: `CREATE FUNCTION test_supply_authority_failure() RETURNS boolean LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'TEST authority query failure'; END $$; CREATE POLICY test_supply_authority_failure ON supply_access_grants AS RESTRICTIVE FOR SELECT USING(CASE WHEN current_setting('app.test_supply_authority_failure',true)='on' THEN test_supply_authority_failure() ELSE true END);`}}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-manager',true),set_config('app.supply_authority_lookup','off',true),set_config('app.test_supply_authority_failure','on',true)`, strings.TrimPrefix(root, "/v1/supply/parties/")).Error; e != nil {
			return e
		}
		return tx.Exec(`DO $$ DECLARE caught boolean:=false; BEGIN BEGIN PERFORM supply_effective_role(current_setting('app.tenant_id')::uuid,current_setting('app.supply_party')::uuid,'user-auditor',ARRAY['auditor'],NULL); EXCEPTION WHEN OTHERS THEN caught:=true; END; IF NOT caught THEN RAISE EXCEPTION 'expected authority error was not raised'; END IF; IF current_setting('app.supply_authority_lookup',true)<>'off' THEN RAISE EXCEPTION 'lookup flag leaked after error'; END IF; END $$;`).Error
	}))
}

func TestAccessRejectsUnsupportedScopeRoleAndPremiseCombinations(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	warehouse := uuid.Must(uuid.NewV7()).String()
	branch := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+warehouse, "owner", premiseBody(0, "IN")).Code)
	branchBody := strings.Replace(strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-SCOPE-BRANCH", 1), `"kind":"warehouse"`, `"kind":"branch"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchBody).Code)
	from := time.Now().Add(-time.Hour)
	until := from.Add(24 * time.Hour)
	for _, role := range []string{"administrator", "purchasing", "finance", "compliance", "sales"} {
		w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-manager", role, warehouse, from, until, "active"))
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, entry := range []struct {
		role    string
		premise any
	}{{"regulator", nil}, {"inventory", branch}, {"auditor", branch}} {
		w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-manager", entry.role, entry.premise, from, until, "active"))
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, role := range []string{"warehouse", "inventory", "picker", "packer", "dispatcher", "driver", "auditor", "regulator"} {
		w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-manager", role, warehouse, from, until, "active"))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
}
