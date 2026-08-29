// Package bootstrap builds the module registry shared by every entrypoint
// that needs one. Before this package existed, cmd/api/main.go hand-wrote
// the []platform.Module{...} literal and cmd/migrate would have had to
// hand-write a second copy of the same list — a drift hazard with no test
// to catch it. Both commands now call Modules() (or NewRegistry()) so the
// list is declared exactly once.
//
// internal/archtest/arch_test.go keeps its own hand-maintained
// allModules() independent of this package on purpose: it is the
// approved-module list a reviewer edits by hand, and
// TestMainRegistersExactlyAllModules fails CI the moment Modules() here
// and allModules() there disagree.
package bootstrap

import (
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/lab"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/patientmaster"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/reference"
	"github.com/tesserix/helivanta/internal/platform"
)

// Modules returns every registered module, in registration order. iam
// must stay first: it owns tenant membership, and the reconciler grants
// tenant_admin its permissions before any other module assumes a member
// row exists.
//
// revocationChecker is handed straight to iam.New so the iam module's
// sign-out/revoke handlers and broadcast consumer invalidate the SAME
// cache instance pkg/authn.Middleware reads on every request (#781).
// cmd/api constructs exactly one *iam.RevocationChecker and passes it
// both here and to authn.Middleware; cmd/migrate, which never serves a
// request, passes nil.
//
// userState is handed to iam.Module.SetUserStateChecker so POST
// /v1/auth/renew (#916, design spec D3) re-checks the SAME Zitadel
// login-client PAT identity cmd/api constructs once, rather than a
// second client. cmd/migrate, which never serves a request, passes nil.
func Modules(revocationChecker *iam.RevocationChecker, userState iam.UserStateChecker) []platform.Module {
	return []platform.Module{
		iam.New(revocationChecker).SetUserStateChecker(userState),
		reference.New(),
		medicore.New(),
		pharmacy.New(),
		lab.New(),
		patientmaster.New(),
	}
}

// NewRegistry builds a *platform.Registry pre-populated with
// Modules(revocationChecker, userState). Both cmd/api and cmd/migrate
// call this instead of hand-rolling the registration loop.
func NewRegistry(revocationChecker *iam.RevocationChecker, userState iam.UserStateChecker) (*platform.Registry, error) {
	reg := platform.NewRegistry()
	for _, m := range Modules(revocationChecker, userState) {
		if err := reg.Register(m); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
