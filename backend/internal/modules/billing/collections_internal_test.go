package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A past day stays unfinal while one of its shifts is open or waiting: a
// night shift that opened on it and has not been counted.
func TestAPastDayIsFinalOnlyWhenEveryShiftIsClosed(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	counted := func(status string, variance int64) collectionShift {
		return collectionShift{Status: status, OpeningFloatMinor: n(0), PaymentsMinor: n(100), DepositsMinor: n(0), RefundsMinor: n(0),
			VoidedCount: n(0), VoidedMinor: n(0), VoidedRefundsMinor: n(0), UpiPaymentsMinor: n(0), UpiDepositsMinor: n(0), PickedUpMinor: n(0),
			CardPaymentsMinor: n(0), CardDepositsMinor: n(0), NetbankingPaymentsMinor: n(0), NetbankingDepositsMinor: n(0), WalletPaymentsMinor: n(0), WalletDepositsMinor: n(0),
			ExpectedMinor: n(100), DeclaredMinor: n(100 + variance), VarianceMinor: n(variance)}
	}
	for name, c := range map[string]struct {
		shifts []collectionShift
		final  bool
	}{
		"all closed":       {[]collectionShift{counted("closed", 0), counted("closed", 5)}, true},
		"one still open":   {[]collectionShift{counted("closed", 0), counted("open", 0)}, false},
		"one waiting":      {[]collectionShift{counted("pending_authorisation", -5)}, false},
		"nothing that day": {nil, true},
	} {
		d := dailyCollection{Final: true, Shifts: c.shifts}
		d.summarise()
		require.Equal(t, c.final, d.Final, name)
	}
}
