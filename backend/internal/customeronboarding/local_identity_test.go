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

func TestLocalRecoveryOwnerRequiresBothExplicitFixturesAndTheDevelopmentIssuer(t *testing.T) {
	for _, tc := range []struct {
		env, issuer, enabled, identity string
		allowed                        bool
	}{
		{"dev", "http://auth.tesserix.localhost:20080", "true", "true", true},
		{"production", "http://auth.tesserix.localhost:20080", "true", "true", false},
		{"dev", "https://auth.tesserix.app", "true", "true", false},
		{"dev", "http://auth.tesserix.localhost:20080", "", "true", false},
		{"dev", "http://auth.tesserix.localhost:20080", "true", "", false},
	} {
		owner, err := NewLocalPasswordRecoveryOwner(tc.env, tc.issuer, tc.enabled, tc.identity)
		if (err == nil) != tc.allowed || (owner != nil) != tc.allowed {
			t.Fatal("unsafe recovery fixture owner accepted")
		}
		if tc.allowed && (owner.origin != "http://helivanta.localhost:4301" || owner.base != "http://127.0.0.1:18787" || !owner.localIdentityFixture) {
			t.Fatal("recovery fixture escaped fixed local destinations")
		}
	}
}
