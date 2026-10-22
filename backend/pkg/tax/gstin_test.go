package tax_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tax"
)

func TestValidateRegistration(t *testing.T) {
	raw, err := os.ReadFile("testdata/gstin.json")
	require.NoError(t, err)
	var doc struct {
		KnownAnswer string `json:"known_answer"`
		Cases       []struct {
			Name, Jurisdiction, Identifier string
			Error                          *string
		}
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	s := india(t)
	require.NoError(t, tax.ValidateRegistration(s, doc.KnownAnswer[:2], "gstin", doc.KnownAnswer))
	want := map[string]error{"format": tax.ErrFormat, "checksum": tax.ErrChecksum, "jurisdiction": tax.ErrJurisdictionMismatch}
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			err := tax.ValidateRegistration(s, c.Jurisdiction, "gstin", c.Identifier)
			if c.Error == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, want[*c.Error])
		})
	}
	require.ErrorIs(t, tax.ValidateRegistration(s, "07", "pan", doc.Cases[0].Identifier), tax.ErrKind)
}

func TestNormalizeRegistration(t *testing.T) {
	require.Equal(t, "07ABCDE1234F1Z2", tax.NormalizeRegistration(" 07abcde1234f1z2 "))
}
