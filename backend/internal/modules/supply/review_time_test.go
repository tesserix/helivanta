package supply

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewWireTimesDoNotDependOnDatabaseOrHostTimezone(t *testing.T) {
	for _, offset := range []int{11 * 3600, -5 * 3600} {
		t.Run(time.FixedZone("fixture", offset).String(), func(t *testing.T) {
			instant := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC).In(time.FixedZone("fixture", offset))
			data, err := json.Marshal(licenseReview{CreatedAt: instant, UpdatedAt: instant})
			require.NoError(t, err)
			var wire struct {
				CreatedAt string `json:"created_at"`
				UpdatedAt string `json:"updated_at"`
			}
			require.NoError(t, json.Unmarshal(data, &wire))
			require.Equal(t, "2026-10-16T00:00:00Z", wire.CreatedAt)
			require.Equal(t, "2026-10-16T00:00:00Z", wire.UpdatedAt)
		})
	}
}
