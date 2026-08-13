package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam"
)

func TestModulesReturnsIAMFirst(t *testing.T) {
	mods := Modules(nil)
	require.NotEmpty(t, mods)
	require.Equal(t, "iam", mods[0].Name(), "iam must register first: it owns tenant membership")
}

func TestModulesHasNoDuplicateNames(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Modules(nil) {
		require.False(t, seen[m.Name()], "duplicate module name %q", m.Name())
		seen[m.Name()] = true
	}
}

func TestNewRegistryRegistersEveryModule(t *testing.T) {
	reg, err := NewRegistry(nil)
	require.NoError(t, err)
	require.Len(t, reg.All(), len(Modules(nil)))
}

// TestNewRegistryGivesIAMTheExactCheckerPassedIn is the wiring guard for
// #781's actual defect: cmd/api/main.go constructs exactly ONE
// *iam.RevocationChecker and must hand that same instance to both
// bootstrap.NewRegistry (which threads it into iam.New) and
// authn.Middleware. Two separately constructed checkers would compile
// and pass nearly every other test in this suite while the module's
// sign-out/revoke handlers silently invalidate a cache the
// authentication path never reads — see
// TestModuleAndMiddlewareShareOneRevocationCheckerInstance in
// internal/modules/iam for the end-to-end version of this same proof.
func TestNewRegistryGivesIAMTheExactCheckerPassedIn(t *testing.T) {
	checker := iam.NewRevocationChecker(nil)

	reg, err := NewRegistry(checker)
	require.NoError(t, err)

	var found *iam.Module
	for _, m := range reg.All() {
		if im, ok := m.(*iam.Module); ok {
			found = im
		}
	}
	require.NotNil(t, found, "iam module must be registered")
	require.Same(t, checker, found.CheckerForTest(),
		"bootstrap.NewRegistry must hand the iam module the exact checker instance the caller passed in")
}
