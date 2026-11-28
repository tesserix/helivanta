// Package gtin validates GS1 product identifiers.
package gtin

import "strings"

// Normalize validates a GTIN-8/12/13/14 and returns its GTIN-14 key.
func Normalize(value string) (string, bool) {
	if !Valid(value) {
		return "", false
	}
	return strings.Repeat("0", 14-len(value)) + value, true
}

// Valid checks the GS1 mod-10 check digit.
func Valid(g string) bool {
	if l := len(g); l != 8 && (l < 12 || l > 14) {
		return false
	}
	sum := 0
	for i := len(g) - 2; i >= 0; i-- {
		d := int(g[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if (len(g)-2-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	last := int(g[len(g)-1] - '0')
	return last >= 0 && last <= 9 && (10-sum%10)%10 == last
}
