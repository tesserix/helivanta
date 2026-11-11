package customeronboarding

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// SigninDeps supplies native identity proof and its current revocation boundary.
type SigninDeps struct {
	Tokens      authn.TokenVerifier
	Revocations authn.RevocationChecker
	Limiter     ratelimit.Limiter
	Limit       ratelimit.Rule
}

// Signin continues an existing-account signup before any tenant session exists.
func (h *Handlers) Signin(deps SigninDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.browser(c) {
			return
		}
		cap, exists, _ := readCapability(c.Request)
		if !exists {
			respond.Error(c, 400, "invalid_request", "start workspace signup before signing in")
			return
		}
		var in customercontract.SigninInput
		if !decode(c, &in) {
			return
		}
		if in.Validate() != nil {
			respond.Error(c, 400, "invalid_request", "invalid sign-in proof")
			return
		}
		if deps.Tokens == nil || deps.Revocations == nil {
			unavailable(c)
			return
		}
		principal, err := deps.Tokens.Verify(c.Request.Context(), in.IDToken)
		proof := customercontract.ExistingOwnerInput{Subject: principal.Subject, AuthTime: principal.AuthTime.Unix(), NextReceiptHash: cap.hash()}
		if err != nil || proof.Validate(time.Now().UTC()) != nil {
			respond.Unauthenticated(c, "a fresh sign-in is required")
			return
		}
		if deps.Limiter != nil {
			decision := deps.Limiter.Allow("signup-existing:"+principal.Subject, deps.Limit, time.Now())
			if !decision.Allowed {
				respond.TooManyRequests(c, "too many sign-in attempts; try again later", decision.RetryAfter, decision.Limit, decision.Remaining)
				return
			}
		} else {
			requestid.Logger(c).WarnContext(c.Request.Context(), "signup sign-in rate limiter unavailable")
		}
		revoked, err := deps.Revocations.RevokedAfter(c.Request.Context(), principal.Subject)
		if err != nil {
			unavailable(c)
			return
		}
		if !revoked.IsZero() && !principal.AuthTime.After(revoked) {
			respond.Unauthenticated(c, "a fresh sign-in is required")
			return
		}
		next, err := newCapability()
		if err != nil {
			unavailable(c)
			return
		}
		next.id = cap.id
		proof.NextReceiptHash = next.hash()
		receipt, _, ok := h.request(c, http.MethodPost, "/"+cap.id.String()+"/existing-owner", cap, proof)
		if !ok {
			return
		}
		setCapability(c, next)
		respond.OK(c, receipt)
	}
}
