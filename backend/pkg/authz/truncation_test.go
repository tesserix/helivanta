package authz

import "testing"

// TestListObjectsTruncated exercises the truncation threshold as a pure
// function against hand-picked counts, independent of any OpenFGA
// container — proving the cap detection is correct without needing to
// write 1000+ tuples into a real store to trigger it. Resolve and
// ListRoles are covered by client_test.go's integration tests for the
// non-truncated path; this is the discriminating test for the boundary
// itself.
func TestListObjectsTruncated(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want bool
	}{
		{"well under the cap", 3, false},
		{"one under the cap", openFGADefaultListObjectsMaxResults - 1, false},
		{"exactly at the cap", openFGADefaultListObjectsMaxResults, true},
		{"over the cap", openFGADefaultListObjectsMaxResults + 1, true},
		{"zero results", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listObjectsTruncated(tt.n); got != tt.want {
				t.Errorf("listObjectsTruncated(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}
