//go:build clamav_acceptance

package attachments_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func TestRealClamAVScansCleanAndSyntheticAntivirusFixtures(t *testing.T) {
	address := os.Getenv("CLAMAV_ACCEPTANCE_ADDRESS")
	require.NotEmpty(t, address, "explicit isolated scanner address is required")
	scanner, err := attachments.NewClamAV(address, 30*time.Second, 1<<20)
	require.NoError(t, err)
	status, err := scanner.Status(t.Context(), time.Now().UTC())
	require.NoError(t, err)
	require.NotEmpty(t, status.EngineVersion)
	require.Positive(t, status.DefinitionVersion)
	const eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, err = z.Write([]byte(eicar))
	require.NoError(t, err)
	require.NoError(t, z.Close())
	for _, tc := range []struct {
		name, content string
		clean         bool
	}{
		{"benign", "Synthetic staff evidence. No source hospital information.", true},
		{"antivirus_test", eicar, false},
		{"compressed_antivirus_test", compressed.String(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, err := scanner.Scan(t.Context(), strings.NewReader(tc.content))
			require.NoError(t, err)
			require.Equal(t, tc.clean, verdict.Clean)
			require.Equal(t, status, verdict.ScannerStatus)
		})
	}
	verdict, err := scanner.Scan(t.Context(), strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	require.Error(t, err)
	require.False(t, verdict.Clean)
}
