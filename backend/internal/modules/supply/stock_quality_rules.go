package supply

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

func validStockQualityInput(in WholesaleQualityInput) bool {
	return in.ExpectedVersion >= 0 && in.ExpectedVersion < 9007199254740991 && in.IdentityCheckID.Version() == 7 && in.IdentityCheckID.Variant() == uuid.RFC4122 && in.IdentityVersion > 0 && in.IdentityVersion <= 9007199254740991 && (in.Decision == "release" || in.Decision == "hold") && validStockText(in.Reason, 500)
}

func decideStockQuality(receipt wholesaleReceiptRow, identity WholesaleIdentityCheck, version int64, in WholesaleQualityInput, actor string, at time.Time) (WholesaleQualityDecision, error) {
	if !validStockQualityInput(in) || !validStockText(actor, 200) || at.IsZero() || version < 0 || version >= 9007199254740991 || version != in.ExpectedVersion || identity.ID != in.IdentityCheckID || identity.Version != in.IdentityVersion || identity.OwnerTenantID != receipt.TenantID || identity.PartyID != receipt.PartyID || identity.PremiseID != receipt.PremiseID || identity.ReceiptID != receipt.ID || identity.Status != "matched" || identity.QuantityBaseUnits < 1 || identity.QuantityBaseUnits > 9007199254740991 {
		return WholesaleQualityDecision{}, errWholesaleReceiptConflict
	}
	status, released := "held", int64(0)
	if in.Decision == "release" {
		// India-only stock owners use the premise's calendar, including UTC rollover.
		today := at.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)
		expiry, err := time.Parse(time.DateOnly, receipt.ExpiresOn)
		if err != nil || expiry.Format(time.DateOnly) <= today || actor == receipt.ActorSubject || actor == identity.ActorSubject || !slices.Contains([]string{"none", "G", "H", "H1"}, identity.Source.RegulatoryClass) || identity.Source.Ndps {
			return WholesaleQualityDecision{}, errWholesaleReceiptConflict
		}
		status, released = "released", identity.QuantityBaseUnits
	}
	return WholesaleQualityDecision{OwnerTenantID: receipt.TenantID, PartyID: receipt.PartyID, PremiseID: receipt.PremiseID, ReceiptID: receipt.ID, Input: in, Version: version + 1, Status: status, ReleasedBaseUnits: released, ActorSubject: actor, CreatedAt: at.UTC().Truncate(time.Microsecond)}, nil
}
