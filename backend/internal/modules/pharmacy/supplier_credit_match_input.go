package pharmacy

import "strings"

func normalizeSupplierCreditMatch(in *SupplierCreditMatchInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.OverageReason != nil {
		value := strings.TrimSpace(*in.OverageReason)
		in.OverageReason = &value
		if !stocktakeText(value, 500) {
			return false
		}
	}
	return validSupplierCreditID(in.ReturnID) && stocktakeText(in.Reason, 500) &&
		in.ExpectedVersion != nil && *in.ExpectedVersion >= 1 && *in.ExpectedVersion < 1000000000000 &&
		in.ExpectedReturnMatchedMinor != nil && *in.ExpectedReturnMatchedMinor >= 0 && *in.ExpectedReturnMatchedMinor <= 1000000000000 &&
		in.AmountMinor != nil && *in.AmountMinor >= 1 && *in.AmountMinor <= 1000000000000-*in.ExpectedReturnMatchedMinor
}

func normalizeSupplierCreditMatchReversal(in *SupplierCreditMatchReversalInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	return stocktakeText(in.Reason, 500) &&
		in.ExpectedVersion != nil && *in.ExpectedVersion >= 1 && *in.ExpectedVersion < 1000000000000 &&
		in.ExpectedReturnMatchedMinor != nil && *in.ExpectedReturnMatchedMinor >= 0 && *in.ExpectedReturnMatchedMinor <= 1000000000000
}
