package platform

import (
	"testing"

	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type fakeModule struct{ name string }

func (f fakeModule) Name() string                          { return f.name }
func (f fakeModule) Migrations() []tenantdb.Migration      { return nil }
func (f fakeModule) Permissions() []authz.Grant            { return nil }
func (f fakeModule) Routes(r *Router, deps Deps)           {}
func (f fakeModule) Consumers(deps Deps) []events.Consumer { return nil }

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeModule{"reference"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(fakeModule{"reference"}); err == nil {
		t.Fatal("expected duplicate module name to be rejected")
	}
	if got := len(r.All()); got != 1 {
		t.Fatalf("All() = %d modules, want 1", got)
	}
}
