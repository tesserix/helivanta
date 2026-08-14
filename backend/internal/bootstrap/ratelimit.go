package bootstrap

import (
	"time"

	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/pkg/ratelimit"
)

// RateLimitConfig builds the rate-limiting policy from cfg. cmd/api calls
// this rather than constructing ratelimit.Config inline so the exemption
// and tight-route allowlists live in exactly one place: the arch test
// (internal/archtest/ratelimit_test.go) calls this same function to pin
// them, rather than a hand-copied map that would only prove someone
// transcribed the routes correctly, not that the route names are real.
//
// Burst is Rate/6 for the tenant and principal rules — ten seconds' worth
// — matching the design spec's reasoning that a burst smaller than a
// page load throttles ordinary navigation (a dashboard resolves
// permissions, then a zone list, then a panel, in quick succession). The
// mint rule's burst is fixed at 3 rather than derived the same way:
// Rate/6 at the default of 10/min would floor to 1, which is smaller
// than the burst the tenant-switch flow itself needs.
func RateLimitConfig(cfg config.Config) ratelimit.Config {
	return ratelimit.Config{
		Tenant: ratelimit.Rule{
			Rate: cfg.RateLimitTenantPerMin, Burst: cfg.RateLimitTenantPerMin / 6, Per: time.Minute,
		},
		Principal: ratelimit.Rule{
			Rate: cfg.RateLimitPrincipalPerMin, Burst: cfg.RateLimitPrincipalPerMin / 6, Per: time.Minute,
		},
		Tight: map[string]ratelimit.Rule{
			// Mints a GIP custom token per call (internal/modules/iam/me.go
			// switchTenant). Identity Platform quota is project-wide, so
			// exhausting it breaks sign-in for every hospital, not just this
			// caller's.
			"POST /v1/iam/me/tenant": {Rate: cfg.RateLimitMintPerMin, Burst: 3, Per: time.Minute},
		},
		Exempt: map[string]string{
			"POST /v1/iam/me/sign-out":              "a clinician on a shared ward terminal must always be able to end their session",
			"POST /v1/iam/subjects/:subject/revoke": "incident response; an attacker looping requests is exactly what would trip the limiter",
		},
	}
}
