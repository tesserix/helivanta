package release

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfessionalSourceRoutesRemainCompiledUnreleased(t *testing.T) {
	count := 0
	for route, capability := range routeCapabilities {
		_, path, _ := strings.Cut(route, " ")
		if !strings.HasPrefix(path, "/v1/practitioners/") {
			continue
		}
		count++
		require.Equalf(t, "unreleased", capability, "professional source must not inherit a released capability: %s", route)
	}
	require.Equal(t, 68, count, "review every newly registered professional route's release boundary")
}
