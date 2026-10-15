package supply_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestPartyAndAccessEventsReachJetStreamWithoutPrivateFieldsOrRetryDuplicates(t *testing.T) {
	r, db, bus, ctx := partySetup(t)
	delivered := make(chan events.Event, 4)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-supply-observer", Subject: "helivanta.in.supply.>", Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error { delivered <- event; return nil }}}))
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	body := structuredPartyBody(0, "TEST Private Synthetic Supplier")
	for range 2 {
		require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", body).Code)
	}
	grant := path + "/access/user-auditor/auditor"
	for range 2 {
		require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	}
	for range 2 {
		require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	}
	seen := map[string]int{}
	for range 3 {
		select {
		case event := <-delivered:
			require.Equal(t, testutil.TenantA, event.TenantID)
			require.NotContains(t, string(event.Data), "TEST")
			require.NotContains(t, string(event.Data), "auditor")
			var data map[string]any
			require.NoError(t, json.Unmarshal(event.Data, &data))
			require.Equal(t, id, data["party_id"])
			switch event.Type {
			case "SupplyPartyUpdated":
				require.Len(t, data, 2)
				require.EqualValues(t, 1, data["version"])
				seen["updated"]++
			case "SupplyPartyAccessChanged":
				require.Len(t, data, 3)
				_, err := uuid.Parse(data["access_id"].(string))
				require.NoError(t, err)
				seen[data["operation"].(string)]++
			default:
				t.Fatalf("unexpected supply event %q", event.Type)
			}
		case <-ctx.Done():
			t.Fatal("supply outbox did not reach the durable NATS consumer")
		}
	}
	require.Equal(t, map[string]int{"updated": 1, "granted": 1, "revoked": 1}, seen)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var eventsCount, audits int64
		if err := tx.Table("outbox_events").Where("subject LIKE 'helivanta.in.supply.%'").Count(&eventsCount).Error; err != nil {
			return err
		}
		if err := tx.Table("audit_events").Where("resource_type='supply_party_access'").Count(&audits).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, eventsCount)
		require.EqualValues(t, 3, audits)
		return nil
	}))
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_access_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_supply_access_outbox_failure CHECK(subject <> 'helivanta.in.supply.party_access_changed.v1') NOT VALID`}}))
	require.Equal(t, 500, testutil.Do(r, "PUT", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "auditor", "").Code)
}
