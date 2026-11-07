package testutil

import (
	"encoding/json"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// The key hashes JSON, so it covers a migration only if JSON round-trips every field, including ones added later.
func TestMigrationKeyEncodingCarriesEveryField(t *testing.T) {
	roundTrips := func(m tenantdb.Migration) bool {
		raw, err := json.Marshal(m)
		if err != nil {
			return false
		}
		var back tenantdb.Migration
		return json.Unmarshal(raw, &back) == nil && reflect.DeepEqual(m, back)
	}
	require.NoError(t, quick.Check(roundTrips, nil))
}

func TestMigrationKeyChangesWithTheMigrations(t *testing.T) {
	a := tenantdb.Migration{ID: "0001_a", SQL: "SELECT 1"}
	b := tenantdb.Migration{ID: "0002_b", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "i", Table: "t", Columns: []tenantdb.IndexColumn{{Name: "c"}}}}}
	key := func(migs ...tenantdb.Migration) string {
		k, err := migrationKey(migs)
		require.NoError(t, err)
		return k
	}
	base := key(a, b)
	require.Equal(t, base, key(a, b))
	descending := b
	descending.ConcurrentIndexes = []tenantdb.ConcurrentIndex{{Name: "i", Table: "t", Columns: []tenantdb.IndexColumn{{Name: "c", Descending: true}}}}
	edited := a
	edited.SQL = "SELECT 2"
	for name, other := range map[string]string{
		"reordered": key(b, a),
		"removed":   key(a),
		"edited":    key(edited, b),
		"index":     key(a, descending),
	} {
		require.NotEqual(t, base, other, name)
	}
}
