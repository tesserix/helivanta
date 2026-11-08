package stockproduct_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/stockproduct"
)

func TestReferencePreservesSharedConsumerWireFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/reference.json")
	require.NoError(t, err)
	var reference stockproduct.Reference
	require.NoError(t, json.Unmarshal(raw, &reference))
	encoded, err := json.Marshal(reference)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(encoded), "Go and TypeScript consume the same owner wire contract without dropped or renamed fields")
}
