package supplierorder

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
)

func TestOrderDecoderAcceptsAllHundredBoundedContractLines(t *testing.T) {
	_, in, _ := orderFixture(t)
	line := in.Lines[0]
	in.Lines = nil
	for range 100 {
		copy := line
		copy.ID = uuid.Must(uuid.NewV7())
		in.Lines = append(in.Lines, copy)
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<10)
	var out SupplierOrderInput
	require.NoError(t, DecodeObject(raw, &out))
	require.Len(t, out.Lines, 100)
	require.ErrorIs(t, suppliertrade.DecodeObject(raw, &out), suppliertrade.ErrInvalid)
}
func TestOrderDecoderRejectsAmbiguousAndOversizedDocuments(t *testing.T) {
	for _, raw := range []string{`{"reason":"a","reason":"b"}`, `[]`, `{} {}`} {
		var out SupplierOrderInput
		require.Error(t, DecodeObject([]byte(raw), &out))
	}
}
