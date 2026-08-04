// Package archtest enforces the architecture rules that lint alone
// cannot express precisely (same-module imports allowed, cross-module
// denied) and the registry-level invariants (unique migration IDs,
// subject/consumer naming).
package archtest

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/modules/reference"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/events"
)

const modulesPrefix = "github.com/tesserix/hms/internal/modules/"

// allModules must list every registered module. cmd/api/main.go is the
// runtime source of truth; keep them in sync (the generator prints a
// reminder).
func allModules() []platform.Module {
	return []platform.Module{reference.New(), medicore.New(), pharmacy.New(), lab.New()}
}

func moduleOf(pkgPath string) string {
	rest := strings.TrimPrefix(pkgPath, modulesPrefix)
	if rest == pkgPath {
		return ""
	}
	return strings.SplitN(rest, "/", 2)[0]
}

func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports}, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		from := moduleOf(p.PkgPath)
		for imp := range p.Imports {
			to := moduleOf(imp)
			if to != "" && to != from {
				t.Errorf("module %q imports module %q (%s -> %s): cross-module data flows only via events", from, to, p.PkgPath, imp)
			}
		}
	}
}

func TestMigrationIDsAreGloballyUnique(t *testing.T) {
	seen := map[string]string{}
	record := func(owner, id string) {
		if prev, dup := seen[id]; dup {
			t.Errorf("migration id %q used by both %s and %s", id, prev, owner)
		}
		seen[id] = owner
	}
	for _, m := range events.Migrations() {
		record("events", m.ID)
	}
	for _, mod := range allModules() {
		for _, m := range mod.Migrations() {
			record(mod.Name(), m.ID)
		}
	}
}

var (
	subjectRe  = regexp.MustCompile(`^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`)
	consumerRe = regexp.MustCompile(`^[a-z]+-[a-z-]+$`)
)

func TestConsumerContracts(t *testing.T) {
	deps := platform.Deps{} // consumers are declared statically; nil deps fine for inspection
	for _, mod := range allModules() {
		for _, c := range mod.Consumers(deps) {
			if !consumerRe.MatchString(c.Name) {
				t.Errorf("module %s consumer name %q must match %s", mod.Name(), c.Name, consumerRe)
			}
			if !subjectRe.MatchString(c.Subject) {
				t.Errorf("module %s consumer subject %q must match %s", mod.Name(), c.Subject, subjectRe)
			}
		}
	}
}

func TestPublishedSubjectConstants(t *testing.T) {
	for name, s := range map[string]string{
		"reference.SubjectPinged":          reference.SubjectPinged,
		"medicore.SubjectVisitCreated":     medicore.SubjectVisitCreated,
		"pharmacy.SubjectDispenseRecorded": pharmacy.SubjectDispenseRecorded,
		"lab.SubjectResultReady":           lab.SubjectResultReady,
	} {
		if !subjectRe.MatchString(s) {
			t.Errorf("%s = %q must match %s", name, s, subjectRe)
		}
	}
}
