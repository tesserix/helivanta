package pharmacy

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"strings"
	"testing"
)

func TestReceivingReleaseRejectsUnsafeCommandBounds(t *testing.T) {
	valid := f.ReceivingReleaseInput{ExpectedSummaryVersion: 1, ToLocationID: uuid.Must(uuid.NewV7()), QuantityUnits: 1, Reason: "TEST checked physical release"}
	require.True(t, receivingReleaseValid(valid))
	for _, test := range []struct {
		name  string
		alter func(*f.ReceivingReleaseInput)
	}{
		{"missing version", func(v *f.ReceivingReleaseInput) { v.ExpectedSummaryVersion = 0 }},
		{"unsafe JS version", func(v *f.ReceivingReleaseInput) { v.ExpectedSummaryVersion = 9007199254740992 }},
		{"missing destination", func(v *f.ReceivingReleaseInput) { v.ToLocationID = uuid.Nil }},
		{"unissued destination", func(v *f.ReceivingReleaseInput) { v.ToLocationID = uuid.New() }},
		{"zero quantity", func(v *f.ReceivingReleaseInput) { v.QuantityUnits = 0 }},
		{"negative quantity", func(v *f.ReceivingReleaseInput) { v.QuantityUnits = -1 }},
		{"unbounded quantity", func(v *f.ReceivingReleaseInput) { v.QuantityUnits = 1000000001 }},
		{"empty physical evidence", func(v *f.ReceivingReleaseInput) { v.Reason = "   " }},
		{"unbounded physical evidence", func(v *f.ReceivingReleaseInput) { v.Reason = strings.Repeat("x", 501) }},
	} {
		t.Run(test.name, func(t *testing.T) { value := valid; test.alter(&value); require.False(t, receivingReleaseValid(value)) })
	}
}
