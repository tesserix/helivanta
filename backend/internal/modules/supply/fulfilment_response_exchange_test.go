package supply

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/events"
	"testing"
	"time"
)

func TestReceivingResponseExchangeRequiresActualSenderRecipientAndCanonicalInput(t *testing.T) {
	id := func() string { return uuid.Must(uuid.NewV7()).String() }
	sender, recipient := id(), id()
	record := map[string]any{"id": id(), "owner_tenant_id": sender, "buyer_tenant_id": recipient, "order_id": id(), "shipment_id": id(), "source_product": "pharmacy", "source_organization_id": id(), "source_location_id": id(), "summary_version": 2, "response": "disputed", "reason": "TEST damaged pack evidence", "actor_subject": "TEST-supplier", "actor_staff_id": id(), "created_at": time.Now().UTC().Truncate(time.Microsecond)}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	event := events.Event{ID: id(), Type: "helivanta.in.supply.supplier_receiving_response_exchange.v1", Version: 1, TenantID: sender, DestinationTenantID: recipient, Data: data}
	_, err = receivingResponseEnvelope(event)
	require.NoError(t, err)
	for _, change := range []func(*events.Event){func(v *events.Event) { v.TenantID = id() }, func(v *events.Event) { v.DestinationTenantID = id() }, func(v *events.Event) { v.Version = 2 }, func(v *events.Event) { v.Type = "wrong" }, func(v *events.Event) { v.Data = append(v.Data[:len(v.Data)-1], []byte(`,"unexpected":true}`)...) }} {
		altered := event
		altered.Data = append(json.RawMessage(nil), event.Data...)
		change(&altered)
		_, err = receivingResponseEnvelope(altered)
		require.Error(t, err)
	}
	for _, change := range []func(){func() { record["summary_version"] = 0 }, func() { record["response"] = "overwrite_hospital_counts" }, func() { record["reason"] = " " }, func() { record["actor_subject"] = "" }} {
		copyRecord := map[string]any{}
		for k, v := range record {
			copyRecord[k] = v
		}
		change()
		event.Data, err = json.Marshal(record)
		require.NoError(t, err)
		_, err = receivingResponseEnvelope(event)
		require.Error(t, err)
		record = copyRecord
	}
}
