package drugmaster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidGTINChecksTheGS1Digit(t *testing.T) {
	for g, want := range map[string]bool{
		"8901234567890": true, "8901234567891": false, "96385074": true, "96385075": false,
		"00012345600012": true, "123": false, "890123456789a": false, "": false,
	} {
		require.Equal(t, want, validGTIN(g), g)
	}
}

func TestDecimalsCompareByValue(t *testing.T) {
	require.Equal(t, "500", canonicalDecimal("500.000000"))
	require.Equal(t, "0.5", canonicalDecimal("0.500000"))
	require.Equal(t, "250", canonicalDecimal("250"))
	require.Equal(t, "120", canonicalDecimal("0120"))
	require.True(t, positiveDecimal("0.25"))
	require.False(t, positiveDecimal("0.000"))
	require.False(t, positiveDecimal("-1"))
	require.False(t, positiveDecimal("123456789"), "beyond numeric(14,6)")
}

func TestConfusableLabels(t *testing.T) {
	require.True(t, confusable("Amoxicillin 500 mg capsule", "Amoxicillin 250 mg capsule"), "identical apart from strength")
	require.True(t, confusable("Hydroxyzine tablet", "Hydralazine tablet") == (levenshtein("hydroxyzine tablet", "hydralazine tablet") <= 2))
	require.True(t, confusable("Celebrex capsule", "Cerebrex capsule"))
	require.False(t, confusable("Amoxicillin 500 mg capsule", "Paracetamol 500 mg tablet"))
	require.Equal(t, 3, levenshtein("kitten", "sitting"))
}

func TestUpperBoundEndsThePrefixRange(t *testing.T) {
	require.Equal(t, "amoy", upperBound("amox"))
	require.Equal(t, "a:", upperBound("a9"))
}
