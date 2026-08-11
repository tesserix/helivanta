package authz

import (
	"testing"

	openfga "github.com/openfga/go-sdk"
	"github.com/stretchr/testify/require"
)

// TestSmallestStoreID is the discriminating test for the split-brain
// fix: it exercises the selection rule as a pure function against
// hand-built, deliberately-ordered input, independent of any OpenFGA
// container or ListStores ordering. In particular the first case lists
// the smallest-ID store LAST, so a "first name match wins" (list-order)
// implementation — the pre-fix behavior — would return the wrong ID
// here, while the correct min-ID implementation returns the right one
// regardless of input order.
func TestSmallestStoreID(t *testing.T) {
	t.Run("min ID appears last in list order", func(t *testing.T) {
		stores := []openfga.Store{
			{Id: "01HZZZZZZZZZZZZZZZZZZZZZZZ", Name: "hms-test"},
			{Id: "01HMMMMMMMMMMMMMMMMMMMMMMM", Name: "hms-test"},
			{Id: "01HAAAAAAAAAAAAAAAAAAAAAAA", Name: "hms-test"}, // smallest, listed last
		}
		id, ok := smallestStoreID(stores, "hms-test")
		require.True(t, ok)
		require.Equal(t, "01HAAAAAAAAAAAAAAAAAAAAAAA", id)
	})

	t.Run("single match", func(t *testing.T) {
		stores := []openfga.Store{
			{Id: "01HSINGLE00000000000000000", Name: "hms-test"},
		}
		id, ok := smallestStoreID(stores, "hms-test")
		require.True(t, ok)
		require.Equal(t, "01HSINGLE00000000000000000", id)
	})

	t.Run("no match", func(t *testing.T) {
		stores := []openfga.Store{
			{Id: "01HOTHER0000000000000000000", Name: "some-other-store"},
		}
		_, ok := smallestStoreID(stores, "hms-test")
		require.False(t, ok)
	})

	t.Run("other names interleaved must be ignored", func(t *testing.T) {
		stores := []openfga.Store{
			{Id: "01HAAAAAAAAAAAAAAAAAAAAAAA", Name: "unrelated-store"}, // smaller ID, wrong name
			{Id: "01HZZZZZZZZZZZZZZZZZZZZZZZ", Name: "hms-test"},
			{Id: "01HBBBBBBBBBBBBBBBBBBBBBBB", Name: "another-unrelated-store"},
			{Id: "01HMMMMMMMMMMMMMMMMMMMMMMM", Name: "hms-test"}, // smallest among hms-test matches
		}
		id, ok := smallestStoreID(stores, "hms-test")
		require.True(t, ok)
		require.Equal(t, "01HMMMMMMMMMMMMMMMMMMMMMMM", id)
	})
}
