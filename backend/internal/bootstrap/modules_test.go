package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

// fakeUserStateChecker is a minimal iam.UserStateChecker for
// TestNewRegistryGivesIAMTheExactUserStateCheckerPassedIn below — this
// test is only about instance identity (require.Same), never about what
// UserState actually answers, so a zero-value stub with no behaviour is
// enough.
type fakeUserStateChecker struct{}

func (*fakeUserStateChecker) UserState(context.Context, string) (loginclient.UserState, error) {
	return loginclient.UserStateActive, nil
}

func TestModulesReturnsIAMFirst(t *testing.T) {
	mods := Modules(nil, nil)
	require.NotEmpty(t, mods)
	require.Equal(t, "iam", mods[0].Name(), "iam must register first: it owns tenant membership")
}

func TestModulesHasNoDuplicateNames(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Modules(nil, nil) {
		require.False(t, seen[m.Name()], "duplicate module name %q", m.Name())
		seen[m.Name()] = true
	}
}

func TestNewRegistryRegistersEveryModule(t *testing.T) {
	reg, err := NewRegistry(nil, nil)
	require.NoError(t, err)
	require.Len(t, reg.All(), len(Modules(nil, nil)))
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

	reg, err := NewRegistry(checker, nil)
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

// TestNewRegistryGivesIAMTheExactUserStateCheckerPassedIn mirrors
// TestNewRegistryGivesIAMTheExactCheckerPassedIn exactly, for the same
// reason and against the same blast radius, worse: cmd/api/main.go
// constructs exactly ONE *loginclient.Client (zitadelLoginClient) and
// must hand that same instance to bootstrap.NewRegistry, which threads
// it into iam.Module.SetUserStateChecker. Dropping that
// .SetUserStateChecker(userState) call entirely compiles, and go test
// ./... stays green, while POST /v1/auth/renew 503s for every clinician
// on every renewal — #916's own symptom, reintroduced by a silent wiring
// regression instead of a SameSite cookie policy (Review Round 1,
// IMPORTANT 2).
func TestNewRegistryGivesIAMTheExactUserStateCheckerPassedIn(t *testing.T) {
	userState := &fakeUserStateChecker{}

	reg, err := NewRegistry(nil, userState)
	require.NoError(t, err)

	var found *iam.Module
	for _, m := range reg.All() {
		if im, ok := m.(*iam.Module); ok {
			found = im
		}
	}
	require.NotNil(t, found, "iam module must be registered")
	require.Same(t, userState, found.UserStateCheckerForTest(),
		"bootstrap.NewRegistry must hand the iam module the exact userState checker instance the caller passed in")
}
