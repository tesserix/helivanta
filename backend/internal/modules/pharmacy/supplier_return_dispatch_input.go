package pharmacy

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

func normalizeSupplierReturnDispatch(in *SupplierReturnDispatchInput) bool {
	in.DocumentReference = strings.TrimSpace(in.DocumentReference)
	in.RecipientName = strings.TrimSpace(in.RecipientName)
	in.Reason = strings.TrimSpace(in.Reason)
	at, err := time.Parse(time.RFC3339Nano, in.HandedOverAt)
	if in.ExpectedVersion == nil || *in.ExpectedVersion != 0 || in.LicenceID.Version() != 7 || in.LicenceID.Variant() != uuid.RFC4122 ||
		!stocktakeText(in.DocumentReference, 100) || !stocktakeText(in.RecipientName, 200) || !stocktakeText(in.Reason, 500) ||
		in.PhysicalDisposition != "handed_to_supplier" || err != nil || at.Year() < 1970 || at.Year() > 9999 || at.Nanosecond()%1000 != 0 {
		return false
	}
	in.HandedOverAt = at.UTC().Format(time.RFC3339Nano)
	return true
}
