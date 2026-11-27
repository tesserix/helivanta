package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

func policyDraftPath(policy, revision string) string {
	base := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/verification-policy-drafts/" + policy + "/revisions"
	if revision != "" {
		base += "/" + revision
	}
	return base
}
func policyDraftBody(version int, label string) string {
	return fmt.Sprintf(`{"expected_version":%d,"label":%q,"second_check_required":true,"review_valid_for_seconds":600,"query_response_target_seconds":1200,"reason":"Synthetic administrative proposal"}`, version, label)
}

func TestVerificationPolicyDraftPersistsRevisionsAndRecoversOriginalIntent(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-steward", "editor")
	policy, first, second := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	original := policyDraftBody(0, "TEST draft one")
	created := do(r, "PUT", policyDraftPath(policy, first), "editor", original)
	require.Equal(t, 201, created.Code, created.Body.String())
	var revision pharmacy.VerificationPolicyDraftRevision
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &revision))
	require.EqualValues(t, 1, revision.Version)
	require.Equal(t, "draft", revision.State)
	require.Equal(t, "not_evaluated", revision.ClinicalAuthority)
	require.Equal(t, "user-editor", revision.PreparedBy)
	require.Equal(t, 201, do(r, "PUT", policyDraftPath(policy, second), "editor", policyDraftBody(1, "TEST draft two")).Code)
	recovered := do(r, "PUT", policyDraftPath(policy, first), "editor", original)
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, created.Body.String(), recovered.Body.String())
	read := do(r, "GET", policyDraftPath(policy, first), "editor", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
	for _, change := range []struct{ policy, revision, actor, body string }{
		{policy, first, "editor", policyDraftBody(0, "changed")},
		{policy, first, "steward", original},
		{uuid.Must(uuid.NewV7()).String(), first, "editor", original},
		{policy, uuid.Must(uuid.NewV7()).String(), "editor", policyDraftBody(0, "stale")},
	} {
		require.Equal(t, 409, do(r, "PUT", policyDraftPath(change.policy, change.revision), change.actor, change.body).Code)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("audit_events").Where("tenant_id=? AND resource_type='pharmacy_verification_policy_drafts'", testutil.TenantA).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestVerificationPolicyDraftScopeAndAuditFailureDoNotDisclose(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	policy, revision := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	path := policyDraftPath(policy, revision)
	require.Equal(t, 201, do(r, "PUT", path, "editor", policyDraftBody(0, "TEST private policy")).Code)
	for _, route := range []string{path, policyDraftPath(policy, ""), strings.Split(path, "/verification-policy-drafts/")[0] + "/verification-policy-drafts"} {
		for _, check := range []struct {
			actor string
			code  int
		}{{"unknown", 401}, {"denied", 403}, {"reader", 403}, {"registrar", 404}, {"steward", 404}} {
			response := do(r, "GET", route, check.actor, "")
			require.Equal(t, check.code, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "TEST private policy")
		}
		foreignParent := strings.Replace(route, pharmaOrganization, uuid.Must(uuid.NewV7()).String(), 1)
		require.Equal(t, 404, do(r, "GET", foreignParent, "editor", "").Code)
	}
	pharmacy.SetReadRecordFault(t)
	for _, route := range []string{path, policyDraftPath(policy, "")} {
		response := do(r, "GET", route, "editor", "")
		require.Equal(t, 503, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "TEST private policy")
	}
	replay := do(r, "PUT", path, "editor", policyDraftBody(0, "TEST private policy"))
	require.Equal(t, 503, replay.Code, replay.Body.String())
	require.NotContains(t, replay.Body.String(), "TEST private policy")
}

func TestVerificationPolicyDraftConcurrentReplayAndStaleRevision(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	policy, revision := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	parallel := func(paths []string, body string) []int {
		start := make(chan struct{})
		codes := make(chan int, len(paths))
		var group sync.WaitGroup
		for _, path := range paths {
			group.Go(func() { <-start; codes <- do(r, "PUT", path, "editor", body).Code })
		}
		close(start)
		group.Wait()
		close(codes)
		result := []int{}
		for code := range codes {
			result = append(result, code)
		}
		return result
	}
	same := []string{}
	for range 6 {
		same = append(same, policyDraftPath(policy, revision))
	}
	created := 0
	for _, code := range parallel(same, policyDraftBody(0, "TEST identical retry")) {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	paths := []string{}
	for range 6 {
		paths = append(paths, policyDraftPath(policy, uuid.Must(uuid.NewV7()).String()))
	}
	created = 0
	for _, code := range parallel(paths, policyDraftBody(1, "TEST concurrent edit")) {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 409, code)
		}
	}
	require.Equal(t, 1, created)
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("pharmacy_verification_policy_drafts").Where("policy_id=?", policy).Count(&count).Error)
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestVerificationPolicyDraftHistoryIsBoundedAndImmutable(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	policy := uuid.Must(uuid.NewV7()).String()
	for version := range 5 {
		response := do(r, "PUT", policyDraftPath(policy, uuid.Must(uuid.NewV7()).String()), "editor", policyDraftBody(version, fmt.Sprintf("TEST revision %d", version)))
		require.Equal(t, 201, response.Code, response.Body.String())
	}
	for _, base := range []string{policyDraftPath(policy, ""), strings.Split(policyDraftPath(policy, ""), "/verification-policy-drafts/")[0] + "/verification-policy-drafts"} {
		seen := map[uuid.UUID]bool{}
		cursor := ""
		for range 4 {
			response := do(r, "GET", base+"?limit=2&cursor="+url.QueryEscape(cursor), "editor", "")
			require.Equal(t, 200, response.Code, response.Body.String())
			var page struct {
				Data []pharmacy.VerificationPolicyDraftRevision
				Page struct {
					HasMore    bool    `json:"has_more"`
					NextCursor *string `json:"next_cursor"`
				}
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
			require.LessOrEqual(t, len(page.Data), 2)
			for _, row := range page.Data {
				require.False(t, seen[row.ID])
				seen[row.ID] = true
				require.Equal(t, "not_evaluated", row.ClinicalAuthority)
			}
			if !page.Page.HasMore {
				break
			}
			require.NotNil(t, page.Page.NextCursor)
			cursor = *page.Page.NextCursor
		}
		require.Len(t, seen, 5)
	}

	for _, sql := range []string{
		"UPDATE pharmacy_verification_policy_drafts SET label='changed' WHERE policy_id=?",
		"DELETE FROM pharmacy_verification_policy_drafts WHERE policy_id=?",
	} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(sql, policy).Error }))
	}
}

func TestVerificationPolicyDraftSQLUsesCanonicalTextAndDurationBounds(t *testing.T) {
	_, db := catalogueHarness(t)
	for _, tc := range []struct {
		name, label, reason string
		duration            int64
		valid               bool
	}{
		{"astral boundary", strings.Repeat("😀", 60), "Synthetic", 9223372036, true},
		{"astral overflow", strings.Repeat("😀", 61), "Synthetic", 600, false},
		{"duration overflow", "Synthetic", "Synthetic", 9223372037, false},
		{"zero duration", "Synthetic", "Synthetic", 0, false},
		{"untrimmed label", " Synthetic", "Synthetic", 600, false},
		{"control in label", "Synthetic\nlabel", "Synthetic", 600, false},
		{"untrimmed reason", "Synthetic", "Synthetic\u00a0", 600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var valid bool
				err := tx.Raw("SELECT pharmacy_verification_policy_draft_contract(0,?,true,?,1200,?)", tc.label, tc.duration, tc.reason).Scan(&valid).Error
				require.NoError(t, err)
				require.Equal(t, tc.valid, valid)
				return nil
			}))
		})
	}
}

func TestVerificationPolicyDraftForcedRLSRequiresCurrentOutletScope(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	policy, revision := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, do(r, "PUT", policyDraftPath(policy, revision), "editor", policyDraftBody(0, "TEST isolated draft")).Code)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("pharmacy_verification_policy_drafts").Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count)
			return nil
		}))
	}
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("pharmacy_verification_policy_drafts").Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}
