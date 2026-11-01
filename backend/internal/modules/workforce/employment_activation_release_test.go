package workforce_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestEmploymentActivationRemainsUnavailableBeforeOperationalRelease(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString() + "/staff/" + uuid.Must(uuid.NewV7()).String()
	for _, methodPath := range []struct{ method, path string }{
		{"GET", base + "/employment-reviews/" + uuid.Must(uuid.NewV7()).String() + "/activation-source?employment_activation=v1"},
		{"PUT", base + "/employment-activations/" + uuid.Must(uuid.NewV7()).String() + "?employment_activation=v1"},
	} {
		response := testutil.Do(r, methodPath.method, methodPath.path, "admin", `{}`)
		require.Equal(t, 503, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"error":"employment_activation_unavailable"`)
	}
}
