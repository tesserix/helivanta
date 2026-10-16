package supply_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func reviewedEvidenceScenario(t *testing.T) evidenceScenario {
	t.Helper()
	return releaseReviewEvidence(t, newEvidenceScenario(t))
}

func releaseReviewEvidence(t *testing.T, s evidenceScenario) evidenceScenario {
	t.Helper()
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), evidenceWorker(t, s), s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), evidenceScanInput(s))
	require.NoError(t, err)
	require.Equal(t, "available", status)
	return s
}
func reviewBody(s evidenceScenario, version int, action string) string {
	method, reference := "not_applicable", ""
	if action == "verify" || action == "reject" {
		method, reference = "authority_contact", "TEST-Authority-check-001"
	}
	return fmt.Sprintf(`{"expected_version":%d,"expected_party_version":1,"expected_premise_version":1,"expected_license_version":1,"evidence_id":%q,"action":%q,"reason":"TEST independent review reason","method":%q,"reference":%q}`, version, evidenceScanInput(s).FileID, action, method, reference)
}
func TestLicenceReviewRequiresIndependentDecisionAndPinsSources(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	for _, subject := range []string{"owner", "auditor"} {
		w := testutil.Do(s.r, "PUT", s.root+"/access/user-"+subject+"/compliance", "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	submitted := s.licence + "/reviews/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(s.r, "PUT", submitted, "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"submitted"`)
	receipt := w.Body.String()
	require.JSONEq(t, receipt, testutil.Do(s.r, "PUT", submitted, "owner", reviewBody(s, 0, "submit")).Body.String())
	decision := s.licence + "/reviews/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(s.r, "PUT", decision, "owner", reviewBody(s, 1, "verify"))
	require.Equal(t, 409, w.Code, w.Body.String(), "maker cannot self-verify, even with compliance access")
	w = testutil.Do(s.r, "PUT", decision, "auditor", reviewBody(s, 1, "verify"))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"verified"`)
	w = testutil.Do(s.r, "GET", s.licence+"/review", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"verified"`)
	require.Contains(t, w.Body.String(), `"trading_approved":false`)
	require.Equal(t, 200, testutil.Do(s.r, "POST", s.path+"/revoke", "owner", "").Code)
	w = testutil.Do(s.r, "GET", s.licence+"/review", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"evidence_unavailable"`)
	var page struct{ Data []json.RawMessage }
	w = testutil.Do(s.r, "GET", s.licence+"/reviews", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
}

func grantReviewer(t *testing.T, s evidenceScenario) {
	t.Helper()
	w := testutil.Do(s.r, "PUT", s.root+"/access/user-auditor/compliance", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
}

func newReview(s evidenceScenario) string {
	return s.licence + "/reviews/" + uuid.Must(uuid.NewV7()).String()
}

func TestLicenceReviewCannotCrossTenantPartyOrRevokedGrant(t *testing.T) {
	t.Parallel()
	s := reviewedEvidenceScenario(t)
	for _, token := range []string{"foreign", "auditor"} {
		for _, path := range []string{s.licence + "/review", s.licence + "/reviews"} {
			require.Equal(t, 404, testutil.Do(s.r, "GET", path, token, "").Code)
		}
		require.Equal(t, 404, testutil.Do(s.r, "PUT", newReview(s), token, reviewBody(s, 0, "submit")).Code)
	}
	grantReviewer(t, s)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Equal(t, 204, testutil.Do(s.r, "DELETE", s.root+"/access/user-auditor/compliance", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(s.r, "PUT", newReview(s), "auditor", reviewBody(s, 1, "verify")).Code)
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.licence+"/review", "auditor", "").Code)
}

func TestLicenceReviewConcurrentReplayCreatesOneAuditedFact(t *testing.T) {
	t.Parallel()
	s := reviewedEvidenceScenario(t)
	path := newReview(s)
	const count = 5
	results := make(chan int, count)
	var start sync.WaitGroup
	start.Add(1)
	for range count {
		go func() {
			start.Wait()
			results <- testutil.Do(s.r, "PUT", path, "owner", reviewBody(s, 0, "submit")).Code
		}()
	}
	start.Done()
	created := 0
	for range count {
		code := <-results
		require.Contains(t, []int{200, 201}, code)
		if code == 201 {
			created++
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 409, testutil.Do(s.r, "PUT", path, "owner", strings.ReplaceAll(reviewBody(s, 0, "submit"), "TEST independent review reason", "TEST conflicting reason")).Code)
	require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("supply_license_reviews").Count(&n).Error)
		require.EqualValues(t, 1, n)
		require.NoError(t, tx.Table("audit_events").Where("resource_type='supply_license_reviews' AND action='insert'").Count(&n).Error)
		require.EqualValues(t, 1, n)
		var payload string
		require.NoError(t, tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject='helivanta.in.supply.license_review_changed.v1'`).Scan(&payload).Error)
		require.NotContains(t, payload, "reason")
		require.NotContains(t, payload, "reference")
		require.NotContains(t, payload, "user-owner")
		require.NoError(t, tx.Table("outbox_events").Where("subject='helivanta.in.supply.license_review_changed.v1'").Count(&n).Error)
		require.EqualValues(t, 1, n)
		return nil
	}))
}

func TestLicenceReviewRejectsChangedSourcesAndRetainsOriginalReceipt(t *testing.T) {
	t.Parallel()
	s := reviewedEvidenceScenario(t)
	grantReviewer(t, s)
	path := newReview(s)
	first := testutil.Do(s.r, "PUT", path, "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, first.Code, first.Body.String())
	w := testutil.Do(s.r, "PUT", s.root, "owner", partyBody(1, "TEST Revised Legal Identity"))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(s.r, "GET", s.licence+"/review", "owner", "")
	require.Contains(t, w.Body.String(), `"effective_status":"review_required"`)
	require.Equal(t, 409, testutil.Do(s.r, "PUT", newReview(s), "auditor", reviewBody(s, 1, "verify")).Code)
	replay := testutil.Do(s.r, "PUT", path, "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	changed := strings.Replace(reviewBody(s, 1, "submit"), `"expected_party_version":1`, `"expected_party_version":2`, 1)
	w = testutil.Do(s.r, "PUT", newReview(s), "owner", changed)
	require.Equal(t, 201, w.Code, w.Body.String())
	w = testutil.Do(s.r, "PUT", newReview(s), "auditor", strings.Replace(reviewBody(s, 2, "verify"), `"expected_party_version":1`, `"expected_party_version":2`, 1))
	require.Equal(t, 201, w.Code, w.Body.String())
	var id string
	require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT premise_id::text FROM supply_licenses WHERE id=?`, evidenceScanInput(s).LicenseID).Scan(&id).Error
	}))
	w = testutil.Do(s.r, "PUT", s.root+"/premises/"+id, "owner", strings.ReplaceAll(premiseBody(1, "IN"), "TEST Warehouse", "TEST Revised Premise"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, testutil.Do(s.r, "GET", s.licence+"/review", "owner", "").Body.String(), `"effective_status":"review_required"`)
}

func TestLicenceReviewHistoryAndDatabaseGuardCannotBeBypassed(t *testing.T) {
	t.Parallel()
	s := reviewedEvidenceScenario(t)
	grantReviewer(t, s)
	require.Equal(t, 200, testutil.Do(s.r, "PUT", s.root+"/access/user-owner/compliance", "owner", "").Code)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	for _, statement := range []string{`UPDATE supply_license_reviews SET reason='TEST rewritten history'`, `DELETE FROM supply_license_reviews`} {
		require.Error(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error { return tx.Exec(statement).Error }))
	}
	err := s.db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_subject','user-owner',true),set_config('app.supply_party',?,true)`, evidenceScanInput(s).PartyID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO supply_license_reviews(tenant_id,party_id,license_id,id,version,party_version,premise_id,premise_version,license_version,evidence_id,action,status,reason,method,reference,actor_subject) VALUES(?,?,?,?,2,1,?,1,1,?,'verify','verified','TEST forged self verification','authority_contact','TEST-REF','user-owner')`, testutil.TenantA, evidenceScanInput(s).PartyID, evidenceScanInput(s).LicenseID, uuid.Must(uuid.NewV7()), row["premise_id"], evidenceScanInput(s).FileID).Error
	})
	require.ErrorContains(t, err, "independent reviewer required")
	for i, action := range []string{"reject", "submit", "verify", "suspend"} {
		token := "auditor"
		if action == "submit" {
			token = "owner"
		}
		w = testutil.Do(s.r, "PUT", newReview(s), token, reviewBody(s, i+1, action))
		require.Equal(t, 201, w.Code, w.Body.String())
	}
	require.Contains(t, testutil.Do(s.r, "GET", s.licence+"/review", "owner", "").Body.String(), `"effective_status":"suspended"`)
	var page struct {
		Data []json.RawMessage
		Page struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
	}
	w = testutil.Do(s.r, "GET", s.licence+"/reviews?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.True(t, page.Page.HasMore)
	w = testutil.Do(s.r, "GET", s.licence+"/reviews?limit=1&cursor="+page.Page.NextCursor, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"status":"suspended"`)
}

func TestLicenceReviewOutboxFailureRollsBackDecisionAndAudit(t *testing.T) {
	t.Parallel()
	s := reviewedEvidenceScenario(t)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_review_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_supply_review_outbox_failure CHECK(subject <> 'helivanta.in.supply.license_review_changed.v1') NOT VALID`}}))
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(s.r, "PUT", s.licence+"/reviews/"+id, "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "test_supply_review_outbox_failure")
	require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("supply_license_reviews").Count(&n).Error)
		require.Zero(t, n)
		require.NoError(t, tx.Table("audit_events").Where("resource_id=?", id).Count(&n).Error)
		require.Zero(t, n)
		return nil
	}))
}

func TestLicenceReviewRefusesInvalidAndUnreleasedSubmissions(t *testing.T) {
	t.Parallel()
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 409, w.Code, w.Body.String())
	for name, change := range map[string]func(map[string]any){
		"tenant injection": func(v map[string]any) { v["tenant_id"] = testutil.TenantB },
		"missing sequence": func(v map[string]any) { delete(v, "expected_version") },
		"blank reason":     func(v map[string]any) { v["reason"] = "     " },
		"control reason":   func(v map[string]any) { v["reason"] = "TEST reason\nnot plain" },
		"unknown method":   func(v map[string]any) { v["method"] = "clamav" },
		"future sequence":  func(v map[string]any) { v["expected_version"] = 9007199254740992.0 },
		"invalid action":   func(v map[string]any) { v["action"] = "approve_trading" },
	} {
		t.Run(name, func(t *testing.T) {
			w := testutil.Do(s.r, "PUT", newReview(s), "owner", changedPartyBody(t, reviewBody(s, 0, "submit"), change))
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
}

func TestLicenceReviewTimestampsAreCanonicalUTC(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	var saved struct {
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.True(t, strings.HasSuffix(saved.CreatedAt, "Z"), saved.CreatedAt)
	require.True(t, strings.HasSuffix(saved.UpdatedAt, "Z"), saved.UpdatedAt)
}

func TestLicenceReviewExplainsInactiveAndInvalidDatePrerequisites(t *testing.T) {
	for _, kind := range []string{"expired", "not_yet_valid", "premise_inactive"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s := newEvidenceScenario(t)
			var premise string
			require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT premise_id::text FROM supply_licenses WHERE id=?`, evidenceScanInput(s).LicenseID).Scan(&premise).Error
			}))
			expected := reviewBody(s, 0, "submit")
			if kind == "premise_inactive" {
				body := strings.Replace(premiseBody(1, "IN"), `"status":"active"`, `"status":"inactive"`, 1)
				w := testutil.Do(s.r, "PUT", s.root+"/premises/"+premise, "owner", body)
				require.Equal(t, 200, w.Code, w.Body.String())
				expected = strings.Replace(expected, `"expected_premise_version":1`, `"expected_premise_version":2`, 1)
			} else {
				issued, expires := "1999-01-01", "2000-01-01"
				if kind == "not_yet_valid" {
					issued, expires = "2199-01-01", "2200-01-01"
				}
				body := strings.ReplaceAll(strings.ReplaceAll(licenseBody(1, premise), "2026-01-01", issued), "2030-12-31", expires)
				w := testutil.Do(s.r, "PUT", s.licence, "owner", body)
				require.Equal(t, 200, w.Code, w.Body.String())
				s.body = strings.Replace(s.body, `"expected_license_version":1`, `"expected_license_version":2`, 1)
				expected = strings.Replace(expected, `"expected_license_version":1`, `"expected_license_version":2`, 1)
			}
			s = releaseReviewEvidence(t, s)
			w := testutil.Do(s.r, "GET", s.licence+"/review", "owner", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"effective_status":"`+kind+`"`)
			require.Equal(t, 409, testutil.Do(s.r, "PUT", newReview(s), "owner", expected).Code)
		})
	}
}

func TestLicenceReviewUsesIndependentPakistanSourcesWithoutApprovingTrade(t *testing.T) {
	s := newEvidenceScenario(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(partyBody(0, "TEST Pakistan Distributor"), `"country_code":"IN"`, `"country_code":"PK"`), "TEST-DISTRIBUTOR", "TEST-PK-DISTRIBUTOR"), "TEST-REG-001", "TEST-PK-REG-001"), "TEST-MH", "TEST-Sindh")
	w := testutil.Do(s.r, "PUT", root, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	premise := uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(s.r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "PK"))
	require.Equal(t, 200, w.Code, w.Body.String())
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(s.r, "PUT", licence, "owner", strings.ReplaceAll(licenseBody(0, premise), "TEST-MH", "TEST-Sindh"))
	require.Equal(t, 200, w.Code, w.Body.String())
	s.root, s.licence, s.path = root, licence, licence+"/evidence/"+uuid.Must(uuid.NewV7()).String()
	s = releaseReviewEvidence(t, s)
	grantReviewer(t, s)
	w = testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	w = testutil.Do(s.r, "PUT", newReview(s), "auditor", reviewBody(s, 1, "verify"))
	require.Equal(t, 201, w.Code, w.Body.String())
	w = testutil.Do(s.r, "GET", licence+"/review", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"verified"`)
	require.Contains(t, w.Body.String(), `"trading_approved":false`)
}

func TestLicenceReviewRefusesAStaleBrowserIdentityBeforeWriting(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	request := httptest.NewRequest("PUT", newReview(s), strings.NewReader(reviewBody(s, 0, "submit")))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Subject", "user-auditor")
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, request)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"session_changed"`)
	require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("supply_license_reviews").Count(&n).Error)
		require.Zero(t, n)
		return nil
	}))
}

func TestLicenceReviewCannotAssumeIndependenceWhenSourceHistoryIsInaccessible(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	grantReviewer(t, s)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hide_review_source", SQL: `ALTER POLICY tenant_isolation ON supply_license_revisions USING(false)`}}))
	w = testutil.Do(s.r, "PUT", newReview(s), "auditor", reviewBody(s, 1, "verify"))
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestLicenceReviewDoesNotOfferActionsWhenSourceHistoryIsInaccessible(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	grantReviewer(t, s)
	w := testutil.Do(s.r, "PUT", newReview(s), "owner", reviewBody(s, 0, "submit"))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hide_review_source", SQL: `ALTER POLICY tenant_isolation ON supply_license_revisions USING(false)`}}))
	w = testutil.Do(s.r, "GET", s.licence+"/review", "auditor", "")
	require.Equal(t, 404, w.Code, w.Body.String())
}
