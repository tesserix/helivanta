package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModulesReturnsIAMFirst(t *testing.T) {
	mods := Modules()
	require.NotEmpty(t, mods)
	require.Equal(t, "iam", mods[0].Name(), "iam must register first: it owns tenant membership")
}

func TestModulesHasNoDuplicateNames(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Modules() {
		require.False(t, seen[m.Name()], "duplicate module name %q", m.Name())
		seen[m.Name()] = true
	}
}

func TestNewRegistryRegistersEveryModule(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	require.Len(t, reg.All(), len(Modules()))
}
