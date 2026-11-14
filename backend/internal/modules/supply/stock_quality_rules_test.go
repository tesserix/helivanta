package supply

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/stockproduct"
)

func TestWholesaleQualityReleaseRequiresIndependentCurrentCount(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tenant, party, premise, receipt, check, version := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.New()
	original := wholesaleReceiptRow{TenantID: tenant, PartyID: party, PremiseID: premise, ID: receipt, ActorSubject: "receiver", WholesaleReceiptInput: WholesaleReceiptInput{ExpiresOn: "2026-11-10"}}
	identity := WholesaleIdentityCheck{ID: check, OwnerTenantID: tenant, PartyID: party, PremiseID: premise, ReceiptID: receipt, Version: 1, QuantityBaseUnits: 100, Source: stockproduct.Reference{VersionID: version, RegulatoryClass: "H"}, ActorSubject: "counter", Status: "matched"}
	input := WholesaleQualityInput{IdentityCheckID: check, IdentityVersion: 1, Decision: "release", Reason: "TEST inspected sealed packs"}
	result, err := decideStockQuality(original, identity, 0, input, "reviewer", at)
	require.NoError(t, err)
	require.Equal(t, "released", result.Status)
	require.Equal(t, int64(100), result.ReleasedBaseUnits)
	require.Equal(t, int64(1), result.Version)
	for _, actor := range []string{"receiver", "counter"} {
		_, err = decideStockQuality(original, identity, 0, input, actor, at)
		require.ErrorIs(t, err, errWholesaleReceiptConflict)
	}
}

func TestWholesaleQualityRefusesChangedScopeVersionAndLocalExpiry(t *testing.T) {
	at := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC) // Already 9 November in India.
	tenant, party, premise, receipt, check := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	base := wholesaleReceiptRow{TenantID: tenant, PartyID: party, PremiseID: premise, ID: receipt, ActorSubject: "receiver", WholesaleReceiptInput: WholesaleReceiptInput{ExpiresOn: "2026-11-10"}}
	matched := WholesaleIdentityCheck{ID: check, OwnerTenantID: tenant, PartyID: party, PremiseID: premise, ReceiptID: receipt, Version: 1, QuantityBaseUnits: 100, Source: stockproduct.Reference{VersionID: uuid.New(), RegulatoryClass: "H"}, ActorSubject: "counter", Status: "matched"}
	input := WholesaleQualityInput{IdentityCheckID: check, IdentityVersion: 1, Decision: "release", Reason: "TEST independent inspection"}
	for _, tc := range []struct {
		name   string
		change func(*wholesaleReceiptRow, *WholesaleIdentityCheck, *WholesaleQualityInput)
	}{
		{"stale quality", func(_ *wholesaleReceiptRow, _ *WholesaleIdentityCheck, in *WholesaleQualityInput) {
			in.ExpectedVersion = 1
		}},
		{"stale identity", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) { id.Version = 2 }},
		{"different count", func(_ *wholesaleReceiptRow, _ *WholesaleIdentityCheck, in *WholesaleQualityInput) {
			in.IdentityCheckID = uuid.Must(uuid.NewV7())
		}},
		{"other tenant", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			id.OwnerTenantID = uuid.New()
		}},
		{"other premise", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			id.PremiseID = uuid.Must(uuid.NewV7())
		}},
		{"other receipt", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			id.ReceiptID = uuid.Must(uuid.NewV7())
		}},
		{"expired today locally", func(r *wholesaleReceiptRow, _ *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			r.ExpiresOn = "2026-11-09"
		}},
		{"restricted X", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			id.Source.RegulatoryClass = "X"
		}},
		{"restricted NDPS", func(_ *wholesaleReceiptRow, id *WholesaleIdentityCheck, _ *WholesaleQualityInput) {
			id.Source.Ndps = true
		}},
		{"empty reason", func(_ *wholesaleReceiptRow, _ *WholesaleIdentityCheck, in *WholesaleQualityInput) { in.Reason = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, id, in := base, matched, input
			tc.change(&r, &id, &in)
			_, err := decideStockQuality(r, id, 0, in, "reviewer", at)
			require.Error(t, err)
		})
	}
}

func TestWholesaleQualityHoldDoesNotReleaseExpiredOrSelfCountedStock(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tenant, party, premise, receipt, check := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	r := wholesaleReceiptRow{TenantID: tenant, PartyID: party, PremiseID: premise, ID: receipt, ActorSubject: "receiver", WholesaleReceiptInput: WholesaleReceiptInput{ExpiresOn: "2020-01-01"}}
	id := WholesaleIdentityCheck{ID: check, OwnerTenantID: tenant, PartyID: party, PremiseID: premise, ReceiptID: receipt, Version: 1, QuantityBaseUnits: 100, ActorSubject: "receiver", Status: "matched"}
	in := WholesaleQualityInput{IdentityCheckID: check, IdentityVersion: 1, ExpectedVersion: 1, Decision: "hold", Reason: "TEST damaged seal"}
	result, err := decideStockQuality(r, id, 1, in, "receiver", at)
	require.NoError(t, err)
	require.Equal(t, "held", result.Status)
	require.Zero(t, result.ReleasedBaseUnits)
	require.Equal(t, int64(2), result.Version)
}
