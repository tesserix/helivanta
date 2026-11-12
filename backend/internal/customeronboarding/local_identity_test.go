package customeronboarding

import "testing"

func TestLocalIdentityResolverRequiresExplicitDevelopmentIssuer(t *testing.T) {
	for _, tc := range []struct {
		env, issuer, enabled string
		ok                   bool
	}{
		{"dev", "http://auth.tesserix.localhost:20080", "true", true},
		{"production", "http://auth.tesserix.localhost:20080", "true", false},
		{"dev", "https://auth.tesserix.app", "true", false},
		{"dev", "http://auth.tesserix.localhost:20080", "", false},
		{"test", "http://auth.tesserix.localhost:20080", "true", false},
	} {
		resolver, err := NewLocalIdentityResolver(tc.env, tc.issuer, tc.enabled)
		if (err == nil) != tc.ok || (resolver != nil) != tc.ok {
			t.Fatal("unsafe local identity resolver accepted")
		}
	}
}
