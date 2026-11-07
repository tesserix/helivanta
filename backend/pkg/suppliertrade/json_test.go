package suppliertrade

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTradePayloadRejectsAmbiguousAndMalformedObjects(t *testing.T) {
	type input struct {
		Endpoint struct {
			ID string `json:"id"`
		} `json:"endpoint"`
		Approved bool `json:"approved"`
	}
	for _, raw := range []string{`{"endpoint":{"id":"one"},"approved":true}`, `{"approved":false,"endpoint":{"id":"one"}}`} {
		var v input
		require.NoError(t, DecodeObject([]byte(raw), &v))
	}
	for _, raw := range []string{`{"endpoint":{"id":"one","id":"two"},"approved":true}`, `{"endpoint":{"id":"one"},"approved":true,"approved":false}`, `{"endpoint":{"id":"one"},"\u0061pproved":true,"approved":false}`, `{"endpoint":{"id":"one"},"unknown":true}`, `{"endpoint":{"id":"one"}} {}`, `[]`, `null`, `"text"`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		var v input
		require.ErrorIs(t, DecodeObject([]byte(raw), &v), ErrInvalid, raw)
	}
}
