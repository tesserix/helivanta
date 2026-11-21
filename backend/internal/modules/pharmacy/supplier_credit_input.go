package pharmacy

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

func normalizeSupplierCreditDocument(in *SupplierCreditDocumentInput) bool {
	in.DocumentReference = strings.ToUpper(strings.TrimSpace(in.DocumentReference))
	in.Reason = strings.TrimSpace(in.Reason)
	date, err := time.Parse(time.DateOnly, in.IssuedOn)
	return validSupplierCreditID(in.ReturnID) && (in.CorrectionOf == nil || validSupplierCreditID(*in.CorrectionOf)) &&
		stocktakeText(in.DocumentReference, 100) && stocktakeText(in.Reason, 500) &&
		err == nil && date.Year() >= 1970 && date.Year() <= 9999 && in.Currency == "INR" &&
		in.AmountMinor != nil && *in.AmountMinor >= 1 && *in.AmountMinor <= 1000000000000
}
func normalizeSupplierCreditVoid(in *SupplierCreditVoidInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	return in.ExpectedVersion != nil && *in.ExpectedVersion >= 1 && *in.ExpectedVersion <= 1000000000000 && stocktakeText(in.Reason, 500)
}
func validSupplierCreditID(id uuid.UUID) bool {
	return id.Version() == 7 && id.Variant() == uuid.RFC4122
}
