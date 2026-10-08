package logging

import "testing"

// Published Verhoeff vectors: 236 has check digit 3, 12345 has check digit 1.
func TestVerhoeffValid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"2363", true},
		{"123451", true},
		{"2364", false},
		{"123452", false},
		{"234567890124", true},
		{"234567890125", false},
		{"", false},
		{"23a3", false},
	} {
		if got := verhoeffValid(tc.in); got != tc.want {
			t.Errorf("verhoeffValid(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
