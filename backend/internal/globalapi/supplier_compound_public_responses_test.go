package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

func (f supplierCompoundFixture) assertPublicReceivingResponses(t *testing.T, operator operatingDutyFixture, shipments []fulfilment.Shipment, department uuid.UUID, receiver string) {
	t.Helper()
	for _, shipment := range shipments {
		source := "/v1/supply/stock-owners/" + shipment.Product.Owner + "/organizations/" + shipment.SourceOrganizationID.String() + "/locations/" + shipment.SourceLocationID.String() + "/supplier-orders/" + shipment.OrderID.String()
		path := source + "/shipments/" + shipment.ID.String() + "/receiving"
		var summary fulfilment.ReceivingSummary
		require.Eventually(t, func() bool {
			response := f.sourceStaffRequest(t, operator.principal, "GET", path, nil)
			require.Contains(t, []int{200, 404}, response.Code, response.Body.String())
			if response.Code != 200 {
				return false
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
			return summary.Closed && summary.Version == 2
		}, 20*time.Second, 100*time.Millisecond)
		require.Equal(t, shipment.BuyerTenantID, summary.OwnerTenantID)
		require.Equal(t, shipment.ID, summary.ShipmentID)
		require.Equal(t, shipment.OrderID, summary.OrderID)
		require.Equal(t, shipment.QuantityUnits, summary.AcceptedUnits+summary.RejectedUnits+summary.ShortUnits)
		access := f.sourceStaffRequest(t, operator.principal, "GET", source+"/receiving-response-access", nil)
		require.Equal(t, 200, access.Code, access.Body.String())
		id := uuid.Must(uuid.NewV7())
		responsePath := path + "/responses/" + id.String()
		input := fulfilment.ReceivingResponseInput{ExpectedSummaryVersion: summary.Version, Response: "confirmed", Reason: "TEST source confirms the closed hospital count"}
		if summary.ShortUnits+summary.RejectedUnits > 0 {
			input.Response = "disputed"
			input.Reason = "TEST source requests review of rejected and short delivery evidence"
		}
		stale := input
		stale.ExpectedSummaryVersion--
		denied := f.sourceStaffRequest(t, operator.principal, "PUT", responsePath, stale)
		require.Equal(t, 409, denied.Code, denied.Body.String())
		recorded := f.sourceStaffRequest(t, operator.principal, "PUT", responsePath, input)
		require.Equal(t, 200, recorded.Code, recorded.Body.String())
		var ack map[string]any
		require.NoError(t, json.Unmarshal(recorded.Body.Bytes(), &ack))
		require.Equal(t, "none", ack["financial_effect"])
		replay := f.sourceStaffRequest(t, operator.principal, "PUT", responsePath, input)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.JSONEq(t, recorded.Body.String(), replay.Body.String())
		changed := input
		changed.Reason = "TEST changed response intent"
		conflict := f.sourceStaffRequest(t, operator.principal, "PUT", responsePath, changed)
		require.Equal(t, 409, conflict.Code, conflict.Body.String())
		own := f.sourceStaffRequest(t, operator.principal, "GET", responsePath, nil)
		require.Equal(t, 200, own.Code, own.Body.String())
		var proof fulfilment.ReceivingResponseRecord
		require.NoError(t, json.Unmarshal(own.Body.Bytes(), &proof))
		require.Equal(t, operator.input.Identity.StaffID, proof.ActorStaffID)
		require.Equal(t, operator.principal.Subject, proof.ActorSubject)
		require.Equal(t, id, proof.ID)
		buyer := "/v1/supply/hospitals/" + shipment.HospitalID.String() + "/departments/" + department.String() + "/outlets/" + shipment.BuyerLocationID.String() + "/purchase-orders/" + shipment.OrderID.String() + "/shipments/" + shipment.ID.String() + "/receiving-responses"
		require.Eventually(t, func() bool {
			page := f.hospitalCall(t, "GET", buyer, receiver, nil, 200)
			rows := page["data"].([]any)
			if len(rows) == 0 {
				return false
			}
			require.Len(t, rows, 1, "replay must not duplicate the directed response")
			shared := rows[0].(map[string]any)
			require.Equal(t, id.String(), shared["id"])
			require.Equal(t, shipment.SourceTenantID.String(), shared["owner_tenant_id"])
			require.Equal(t, shipment.BuyerTenantID.String(), shared["buyer_tenant_id"])
			require.Equal(t, input.Response, shared["response"])
			require.Equal(t, float64(summary.Version), shared["summary_version"])
			require.NotContains(t, shared, "actor_staff_id", "source private Staff identity must not be disclosed")
			return true
		}, 20*time.Second, 100*time.Millisecond)
		current := f.hospitalCall(t, "GET", "/v1/pharmacy/hospitals/"+shipment.HospitalID.String()+"/departments/"+department.String()+"/outlets/"+shipment.BuyerLocationID.String()+"/supplier-shipments/"+shipment.ID.String()+"/receiving", receiver, nil, 200)["summary"].(map[string]any)
		require.Equal(t, float64(summary.Version), current["version"])
		require.Equal(t, float64(summary.AcceptedUnits), current["accepted_units"])
		require.Equal(t, float64(summary.RejectedUnits), current["rejected_units"])
		require.Equal(t, float64(summary.ShortUnits), current["short_units"])
	}
}
