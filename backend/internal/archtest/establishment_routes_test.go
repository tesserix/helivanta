package archtest

import (
	"strings"
	"testing"
)

// ADR-0054: a route naming an establishment must admit only that establishment's kind.
var establishmentFamilies = map[string]string{
	"/workforce/suppliers/:supplier":  "supplier",
	"/workforce/hospitals/:hospital":  "hospital",
	"/workforce/pharmacies/:pharmacy": "pharmacy",
}

func TestEveryEstablishmentRouteAdmitsOnlyItsKind(t *testing.T) {
	counts := map[string]int{}
	for _, route := range allDeclaredRoutes(t) {
		want := ""
		for prefix, kind := range establishmentFamilies {
			if route.Path == prefix || strings.HasPrefix(route.Path, prefix+"/") {
				want = kind
				counts[kind]++
			}
		}
		if route.EstablishmentKind != want {
			t.Errorf("%s %s admits establishment kind %q, want %q", route.Method, route.Path, route.EstablishmentKind, want)
		}
	}
	for _, kind := range establishmentFamilies {
		if counts[kind] == 0 {
			t.Errorf("no %s establishment route is registered", kind)
		}
	}
}
