package supply_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func deliveryBody(version int, driver, status string) string {
	fields := map[string]any{"expected_version": version, "code": "TEST-DELIVERY", "source_reference": "TEST-MANUAL-LOGISTICS-REF", "destination_business_name": "TEST Business Warehouse", "destination_line1": "TEST Industrial Street", "destination_line2": "", "destination_city": "TEST Pune", "destination_region": "TEST Maharashtra", "destination_postal_code": "TEST-001", "country_code": "IN", "business_contact_name": "TEST Dispatch Desk", "business_contact_phone": "+919999999999", "window_start": "2030-01-01T09:00:00Z", "window_end": "2030-01-01T17:00:00Z", "package_count": 2, "handling_notes": "TEST Administrative logistics only", "driver_subject": driver, "assignment_expires_at": "", "status": status}
	if driver != "" {
		fields["assignment_expires_at"] = time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	}
	fields["change_reason"] = "TEST Authorized logistics scheduling"
	b, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestDeliveryTaskShowsOnlyCurrentAssignedDriverProjection(t *testing.T) {
	r, _ := operatingHarness(t)
	party := structureParty(t, r, "TEST-DELIVERY-OWNER", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	operatingGrant(t, r, party, premise, "dispatcher", "dispatcher")
	operatingGrant(t, r, party, premise, "driver", "driver")
	path := party + "/premises/" + premise + "/delivery-tasks/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "dispatcher", deliveryBody(0, "user-driver", "assigned"))
	require.Equal(t, 200, w.Code, w.Body.String())
	myPath := party + "/premises/" + premise + "/my-deliveries/" + stringsLast(path)
	w = testutil.Do(r, "GET", myPath, "driver", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"allowed_actions":["start","exception"]`)
	require.NotContains(t, w.Body.String(), "source_reference")
	require.NotContains(t, w.Body.String(), "driver_subject")
	require.Equal(t, 404, testutil.Do(r, "GET", path, "driver", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", myPath, "other-driver", "").Code)
}

func stringsLast(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func deliveryFixture(t *testing.T) (*gin.Engine, *tenantdb.DB, string, string, string, string) {
	t.Helper()
	r, db := operatingHarness(t)
	party := structureParty(t, r, "TEST-DELIVERY-FIXTURE", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	for _, token := range []string{"dispatcher", "driver", "other-driver"} {
		role := "driver"
		if token == "dispatcher" {
			role = "dispatcher"
		}
		operatingGrant(t, r, party, premise, token, role)
	}
	path := party + "/premises/" + premise + "/delivery-tasks/" + uuid.Must(uuid.NewV7()).String()
	body := deliveryBody(0, "user-driver", "assigned")
	w := testutil.Do(r, "PUT", path, "dispatcher", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	return r, db, party, premise, path, body
}

func driverActionBody(version, generation int, action string) string {
	b, err := json.Marshal(map[string]any{"expected_version": version, "expected_assignment_generation": generation, "action": action, "reason": "TEST Authorized delivery action"})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestDeliveryActionsAreConcurrentRetrySafeAndPublishOnlyMinimalEvents(t *testing.T) {
	r, db, party, premise, path, _ := deliveryFixture(t)
	id := stringsLast(path)
	my := party + "/premises/" + premise + "/my-deliveries/" + id
	command := my + "/actions/" + uuid.Must(uuid.NewV7()).String()
	body := driverActionBody(1, 1, "start")
	var start sync.WaitGroup
	start.Add(1)
	responses := make(chan string, 4)
	codes := make(chan int, 4)
	for range 4 {
		go func() {
			start.Wait()
			w := testutil.Do(r, "PUT", command, "driver", body)
			codes <- w.Code
			responses <- w.Body.String()
		}()
	}
	start.Done()
	first := ""
	for range 4 {
		require.Equal(t, 200, <-codes)
		response := <-responses
		if first == "" {
			first = response
		} else {
			require.JSONEq(t, first, response)
		}
	}
	require.Contains(t, first, `"status":"in_transit"`)
	require.NotContains(t, first, "source_reference")
	w := testutil.Do(r, "PUT", command, "driver", strings.Replace(body, "Authorized", "Changed", 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", my+"/actions/"+uuid.Must(uuid.NewV7()).String(), "driver", body).Code)
	partyID := strings.TrimPrefix(party, "/v1/supply/parties/")
	var counts struct{ Revisions, Events, Commands, Audits int }
	require.NoError(t, partyTransaction(t, db, partyID, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT (SELECT count(*) FROM supply_delivery_revisions WHERE delivery_id=?) revisions,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.delivery_task_changed.v1' AND payload->'data'->>'delivery_id'=?) events,(SELECT count(*) FROM supply_delivery_commands WHERE delivery_id=?) commands,(SELECT count(*) FROM audit_events WHERE resource_type='supply_delivery_tasks' AND resource_id=?) audits`, id, id, id, id).Scan(&counts).Error; err != nil {
			return err
		}
		var payloads []string
		if err := tx.Table("outbox_events").Select("payload::text").Where("subject='helivanta.in.supply.delivery_task_changed.v1'").Pluck("payload::text", &payloads).Error; err != nil {
			return err
		}
		for _, payload := range payloads {
			require.NotContains(t, payload, "destination")
			require.NotContains(t, payload, "contact")
			require.NotContains(t, payload, "source_reference")
		}
		return nil
	}))
	require.Equal(t, 2, counts.Revisions)
	require.Equal(t, 2, counts.Events)
	require.Equal(t, 1, counts.Commands)
	require.Equal(t, 2, counts.Audits)
	w = testutil.Do(r, "PUT", my+"/actions/"+uuid.Must(uuid.NewV7()).String(), "driver", driverActionBody(2, 1, "deliver"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"delivered"`)
	require.Contains(t, w.Body.String(), `"allowed_actions":[]`)
}

func TestDeliveryReassignmentInvalidatesOldDriverBeforeCommandReplay(t *testing.T) {
	r, _, party, premise, path, body := deliveryFixture(t)
	id := stringsLast(path)
	my := party + "/premises/" + premise + "/my-deliveries/" + id
	command := my + "/actions/" + uuid.Must(uuid.NewV7()).String()
	start := driverActionBody(1, 1, "start")
	w := testutil.Do(r, "PUT", command, "driver", start)
	require.Equal(t, 200, w.Code, w.Body.String())
	changed := changedPartyBody(t, body, func(v map[string]any) { v["expected_version"] = 2; v["driver_subject"] = "user-other-driver" })
	w = testutil.Do(r, "PUT", path, "dispatcher", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"assignment_generation":2`)
	require.Equal(t, 404, testutil.Do(r, "GET", my, "driver", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", command, "driver", start).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", my, "foreign", "").Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", my+"/actions/"+uuid.Must(uuid.NewV7()).String(), "other-driver", driverActionBody(3, 1, "start")).Code)
	w = testutil.Do(r, "PUT", my+"/actions/"+uuid.Must(uuid.NewV7()).String(), "other-driver", driverActionBody(3, 2, "start"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/revisions", "other-driver", "").Code)
}

func TestDeliverySourceInputAndLocationBoundariesRejectDuplicateOrPrivateFields(t *testing.T) {
	r, _, party, premise, path, body := deliveryFixture(t)
	require.JSONEq(t, testutil.Do(r, "GET", path, "dispatcher", "").Body.String(), testutil.Do(r, "PUT", path, "dispatcher", body).Body.String())
	duplicate := party + "/premises/" + premise + "/delivery-tasks/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", duplicate, "dispatcher", strings.Replace(body, `"TEST-DELIVERY"`, `"TEST-DIFFERENT-CODE"`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	private := changedPartyBody(t, body, func(v map[string]any) { v["patient_name"] = "TEST Patient" })
	require.Equal(t, 400, testutil.Do(r, "PUT", duplicate, "dispatcher", private).Code)
	wrong := changedPartyBody(t, body, func(v map[string]any) { v["window_end"] = "2020-01-01T00:00:00Z" })
	require.Equal(t, 400, testutil.Do(r, "PUT", duplicate, "dispatcher", wrong).Code)
	wrong = changedPartyBody(t, body, func(v map[string]any) { v["country_code"] = "PK" })
	require.Equal(t, 400, testutil.Do(r, "PUT", duplicate, "dispatcher", wrong).Code)
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-finance", "finance", nil, from, from.Add(24*time.Hour), "active")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "finance", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "finance", body).Code)
	other := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+other, "owner", strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-OTHER", 1)).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", party+"/premises/"+other+"/my-deliveries/"+stringsLast(path), "driver", "").Code)
	w = testutil.Do(r, "GET", party+"/premises/"+premise+"/delivery-drivers?limit=1", "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"has_more":true`)
}

func TestDeliveryOutboxFailureRollsBackStatusCommandAndHistory(t *testing.T) {
	r, db, party, premise, path, _ := deliveryFixture(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_delivery_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_delivery_outbox_failure CHECK(subject <> 'helivanta.in.supply.delivery_task_changed.v1') NOT VALID`}}))
	my := party + "/premises/" + premise + "/my-deliveries/" + stringsLast(path)
	w := testutil.Do(r, "PUT", my+"/actions/"+uuid.Must(uuid.NewV7()).String(), "driver", driverActionBody(1, 1, "start"))
	require.Equal(t, 500, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", my, "driver", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"assigned"`)
	require.Contains(t, w.Body.String(), `"version":1`)
}

func TestDeliveryDriverRevocationAndExpiredGrantDenyStaleSessionsAndReplay(t *testing.T) {
	for _, mode := range []string{"revoked", "expired"} {
		t.Run(mode, func(t *testing.T) {
			r, db, party, premise, path, _ := deliveryFixture(t)
			my := party + "/premises/" + premise + "/my-deliveries/" + stringsLast(path)
			command := my + "/actions/" + uuid.Must(uuid.NewV7()).String()
			body := driverActionBody(1, 1, "start")
			w := testutil.Do(r, "PUT", command, "driver", body)
			require.Equal(t, 200, w.Code, w.Body.String())
			w = testutil.Do(r, "GET", party+"/grants", "owner", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			var grants struct {
				Data []struct{ ID, Subject string }
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &grants))
			grantID := ""
			for _, grant := range grants.Data {
				if grant.Subject == "user-driver" {
					grantID = grant.ID
				}
			}
			require.NotEmpty(t, grantID)
			if mode == "revoked" {
				revokeOperatingGrant(t, r, party+"/grants/"+grantID)
			} else {
				partyID := strings.TrimPrefix(party, "/v1/supply/parties/")
				revision := uuid.Must(uuid.NewV7()).String()
				query := fmt.Sprintf(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.tenant_id','%s',true),set_config('app.supply_party','%s',true),set_config('app.supply_subject','user-owner',true),set_config('app.supply_access_admin','on',true); UPDATE supply_access_grants SET expires_at=clock_timestamp()-interval '1 minute',version=version+1 WHERE id='%s'; INSERT INTO supply_access_grant_revisions(tenant_id,party_id,grant_id,id,version,snapshot,actor_subject) SELECT tenant_id,party_id,id,'%s',version,to_jsonb(g),'user-owner' FROM supply_access_grants g WHERE id='%s';`, testutil.TenantA, partyID, grantID, revision, grantID)
				require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_delivery_expired_grant", SQL: query}}))
			}
			require.Equal(t, 404, testutil.Do(r, "GET", my, "driver", "").Code)
			require.Equal(t, 404, testutil.Do(r, "PUT", command, "driver", body).Code)
			require.Equal(t, 200, testutil.Do(r, "GET", path, "dispatcher", "").Code)
		})
	}
}

func TestDeliveryOrdinaryRoleCannotChangeBusinessDetailsOrReadPrivateHistory(t *testing.T) {
	r, db, party, premise, path, _ := deliveryFixture(t)
	partyID := strings.TrimPrefix(party, "/v1/supply/parties/")
	id := stringsLast(path)
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-driver',true)`, partyID).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE supply_delivery_tasks SET destination_business_name='TEST Forbidden edit',version=version+1 WHERE id=?`, id).Error
	})
	require.Error(t, err)
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg))
	require.Equal(t, "supply_delivery_authority", pg.ConstraintName)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-driver',true)`, partyID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("supply_delivery_revisions").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		var allowed bool
		if err := tx.Raw(`SELECT supply_delivery_assigned(?,?,?,'user-driver',clock_timestamp()-interval '1 second')`, testutil.TenantA, partyID, premise).Scan(&allowed).Error; err != nil {
			return err
		}
		require.False(t, allowed)
		return nil
	}))
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_delivery_visible_tenant", SQL: `CREATE OR REPLACE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;`}}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-driver',true)`, partyID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("supply_delivery_tasks").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
	require.NotContains(t, testutil.Do(r, "GET", path, "dispatcher", "").Body.String(), "TEST Forbidden edit")
}
