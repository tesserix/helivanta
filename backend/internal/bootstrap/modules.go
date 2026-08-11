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
	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/modules/reference"
	"github.com/tesserix/hms/internal/platform"
)

// Modules returns every registered module, in registration order. iam
// must stay first: it owns tenant membership, and the reconciler grants
// tenant_admin its permissions before any other module assumes a member
// row exists.
func Modules() []platform.Module {
	return []platform.Module{
		iam.New(),
		reference.New(),
		medicore.New(),
		pharmacy.New(),
		lab.New(),
	}
}

// NewRegistry builds a *platform.Registry pre-populated with Modules().
// Both cmd/api and cmd/migrate call this instead of hand-rolling the
// registration loop.
func NewRegistry() (*platform.Registry, error) {
	reg := platform.NewRegistry()
	for _, m := range Modules() {
		if err := reg.Register(m); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
