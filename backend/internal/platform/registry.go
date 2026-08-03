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
// failure, not a silent bug (issue #2 edge case).
func (r *Registry) Register(m Module) error {
	if _, dup := r.byName[m.Name()]; dup {
		return fmt.Errorf("module %q registered twice", m.Name())
	}
	r.byName[m.Name()] = m
	r.ordered = append(r.ordered, m)
	return nil
}

func (r *Registry) All() []Module { return append([]Module(nil), r.ordered...) }
