package bootstrap

import (
	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/ratelimit"
)

// V1Chain is the middleware chain mounted under /v1, in order. It exists
// as a function rather than a literal inside cmd/api/main.go for one
// reason: main.go's run() opens a database, a NATS connection and an OIDC
// provider before it ever builds a router, so no test can reach the chain
// it mounts. While the chain lived there, deleting ratelimit.Middleware
// from it removed rate limiting from production entirely and the whole
// backend suite stayed green — the placement test built its own
// equivalent chain and so kept passing against a replica of a chain
// production no longer had.
//
// internal/archtest builds its harness from THIS function, so the
// assertion that a throttled request makes no OpenFGA call is made
// against the chain that actually serves traffic. Per
// docs/standards/engineering-principles.md §4, that turns "remember to
// keep the test's chain in step with main.go" from a convention into a
// CI failure.
//
// Order is load-bearing and is the thing the arch test pins:
//
//  1. authn      — verifies the token; everything downstream needs the
//     principal, and the tenant bucket key is only trustworthy because it
//     comes from a verified token rather than a client header.
//  2. requestid  — attaches the principal to the request-scoped logger so
//     a rate-limit denial is attributable.
//  3. ratelimit  — refuses over-budget requests BEFORE the expensive step.
//  4. authz      — calls OpenFGA on every request. It is both the most
//     expensive step in the chain and itself a shared resource, so
//     limiting after it would let a flood exhaust OpenFGA before anything
//     was refused.
func V1Chain(
	verifier authn.TokenVerifier,
	revocations authn.RevocationChecker,
	limiter ratelimit.Limiter,
	cfg config.Config,
	resolver authz.Resolver,
) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		authn.Middleware(verifier, revocations),
		requestid.PrincipalMiddleware(),
		ratelimit.Middleware(limiter, RateLimitConfig(cfg)),
		authz.Middleware(resolver),
	}
}
