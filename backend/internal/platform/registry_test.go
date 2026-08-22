package platform

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type fakeModule struct {
	name          string
	publishes     []string
	directed      []string
	directedWrite []string
}

func (f *fakeModule) Name() string                            { return f.name }
func (f *fakeModule) Migrations() []tenantdb.Migration        { return nil }
func (f *fakeModule) Permissions() []authz.Grant              { return nil }
func (f *fakeModule) Routes(r *Router, deps Deps)             {}
func (f *fakeModule) Publishes() []string                     { return f.publishes }
func (f *fakeModule) DirectedSubjects() []string              { return f.directed }
func (f *fakeModule) DirectedWriteTables() []string           { return f.directedWrite }
func (f *fakeModule) Consumers(deps Deps) []events.Consumer   { return nil }
func (f *fakeModule) Broadcasts(deps Deps) []events.Broadcast { return nil }

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeModule{name: "reference"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(&fakeModule{name: "reference"}); err == nil {
		t.Fatal("expected duplicate module name to be rejected")
	}
	if got := len(r.All()); got != 1 {
		t.Fatalf("All() = %d modules, want 1", got)
	}
}

// TestRegisterRejectsDirectedSubjectNotPublished makes a misdeclaration a
// BOOT failure rather than a runtime surprise. A subject declared
// directed but never published is either a typo or a deleted event, and
// either way the allowlist would be permitting something that no longer
// means what it says.
func TestRegisterRejectsDirectedSubjectNotPublished(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&fakeModule{
		name:      "bad",
		publishes: []string{"helivanta.in.bad.thing.v1"},
		directed:  []string{"helivanta.in.bad.typo.v1"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "helivanta.in.bad.typo.v1")
}

func TestRegisterAcceptsDirectedSubjectThatIsPublished(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&fakeModule{
		name:      "good",
		publishes: []string{"helivanta.in.good.thing.v1"},
		directed:  []string{"helivanta.in.good.thing.v1"},
	}))
	require.Equal(t, []string{"helivanta.in.good.thing.v1"}, r.DirectedSubjects())
}

// TestDirectedWriteTablesIsUnionAcrossModules mirrors
// TestRegisterAcceptsDirectedSubjectThatIsPublished's shape: the registry's
// declared set is the union of every registered module's own declaration,
// not just the last one registered.
func TestDirectedWriteTablesIsUnionAcrossModules(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&fakeModule{
		name:          "first",
		directedWrite: []string{"first_table"},
	}))
	require.NoError(t, r.Register(&fakeModule{
		name:          "second",
		directedWrite: []string{"second_table"},
	}))
	require.Equal(t, []string{"first_table", "second_table"}, r.DirectedWriteTables())
}
