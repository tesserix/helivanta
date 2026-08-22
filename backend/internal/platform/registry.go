package platform

import "fmt"

type Registry struct {
	byName  map[string]Module
	ordered []Module
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]Module{}}
}

// Register fails on duplicate names so route shadowing is a boot
// failure, not a silent bug (issue #2 edge case), and on a directed
// subject the module does not publish (#932): the allowlist may only
// name events that actually exist.
func (r *Registry) Register(m Module) error {
	if _, dup := r.byName[m.Name()]; dup {
		return fmt.Errorf("module %q registered twice", m.Name())
	}
	published := make(map[string]struct{}, len(m.Publishes()))
	for _, s := range m.Publishes() {
		published[s] = struct{}{}
	}
	for _, d := range m.DirectedSubjects() {
		if _, ok := published[d]; !ok {
			return fmt.Errorf("module %q declares %q directed but does not publish it", m.Name(), d)
		}
	}
	r.byName[m.Name()] = m
	r.ordered = append(r.ordered, m)
	return nil
}

func (r *Registry) All() []Module { return append([]Module(nil), r.ordered...) }

// DirectedSubjects is the union of every registered module's
// declarations — the exact set main.go hands to events.NewBus, so
// the allowlist the bus enforces and the one modules declare cannot
// diverge.
func (r *Registry) DirectedSubjects() []string {
	var out []string
	for _, m := range r.ordered {
		out = append(out, m.DirectedSubjects()...)
	}
	return out
}

// DirectedWriteTables is the union of every registered module's
// declarations, for DB.LintDirectedProvenance.
func (r *Registry) DirectedWriteTables() []string {
	var out []string
	for _, m := range r.ordered {
		out = append(out, m.DirectedWriteTables()...)
	}
	return out
}
