package workforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedeliveryDelay(t *testing.T) {
	for attempts, want := range map[int]time.Duration{1: 2 * time.Minute, 2: 4 * time.Minute, 3: 8 * time.Minute, 4: 16 * time.Minute, 5: 32 * time.Minute, 6: time.Hour, 20: time.Hour} {
		require.Equal(t, want, redeliveryDelay(attempts), "attempts=%d", attempts)
	}
}
