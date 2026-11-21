package pharmacy

import (
	"strings"
	"time"
)

func normalizeSupplierCreditAllocation(in *SupplierCreditAllocationInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	return validSupplierCreditID(in.PayableID) && stocktakeText(in.Reason, 500) &&
		validSupplierCreditAllocationObservation(in.ExpectedVersion, in.ExpectedAvailableMinor, in.ExpectedPayableVersion, in.ExpectedOutstandingMinor, in.OccurredOn) &&
		in.AmountMinor != nil && *in.AmountMinor > 0 && *in.AmountMinor <= *in.ExpectedAvailableMinor && *in.AmountMinor <= *in.ExpectedOutstandingMinor
}

func validSupplierCreditAllocationObservation(version, available, payableVersion, outstanding *int64, occurredOn string) bool {
	date, err := time.Parse(time.DateOnly, occurredOn)
	return version != nil && *version >= 1 && *version < 1000000000000 && payableVersion != nil && *payableVersion >= 1 && *payableVersion < 1000000000000 &&
		available != nil && *available >= 0 && *available <= 1000000000000 && outstanding != nil && *outstanding >= 0 && *outstanding <= 1000000000000 && err == nil && date.Year() >= 1970 && date.Year() <= 9999
}

func normalizeSupplierCreditAllocationReversal(in *SupplierCreditAllocationReversalInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	return stocktakeText(in.Reason, 500) && validSupplierCreditAllocationObservation(in.ExpectedVersion, in.ExpectedAvailableMinor, in.ExpectedPayableVersion, in.ExpectedOutstandingMinor, in.OccurredOn)
}
