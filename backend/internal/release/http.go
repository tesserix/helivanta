package release

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"time"
)

// ValidateRoutes makes an unclassified registered endpoint a boot-time failure.
func (s *Service) ValidateRoutes(routes gin.RoutesInfo) error {
	for _, route := range routes {
		if _, ok := RouteCapability(route.Method, route.Path); !ok {
			return fmt.Errorf("unclassified release route: %s %s", route.Method, route.Path)
		}
	}
	return nil
}

// ValidateCompleteRoutes also prevents a promised baseline route being omitted at boot.
func (s *Service) ValidateCompleteRoutes(routes gin.RoutesInfo) error {
	if err := s.ValidateRoutes(routes); err != nil {
		return err
	}
	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
	}
	for key := range routeCapabilities {
		if !registered[key] {
			return fmt.Errorf("missing release route: %s", key)
		}
	}
	return nil
}

// Admission supplements existing authentication and product authorization.
func (s *Service) Admission(method, path string) gin.HandlerFunc {
	key, known := RouteCapability(method, path)
	return func(c *gin.Context) {
		if !known {
			unavailable(c)
			return
		}
		if key == "baseline" {
			c.Next()
			return
		}
		var available bool
		if method == http.MethodPost && path == "/v1/auth/signup" {
			snapshot, err := s.PublicSnapshot(c.Request.Context())
			if err != nil {
				unavailable(c)
				return
			}
			available = snapshot.Onboarding
		} else {
			principal, ok := authn.PrincipalFrom(c)
			if !ok {
				respond.Unauthenticated(c, "Sign in to continue.")
				return
			}
			tenant, err := uuid.Parse(principal.TenantID)
			if err != nil || tenant == uuid.Nil {
				respond.Unauthenticated(c, "Sign in to continue.")
				return
			}
			snapshot, err := s.Snapshot(c.Request.Context(), tenant, principal.Subject)
			if err != nil {
				unavailable(c)
				return
			}
			for _, capability := range snapshot.Capabilities {
				if capability.Key == key {
					available = capability.Available
					break
				}
			}
		}
		if !available {
			respond.Error(c, http.StatusNotFound, "not_found", "This feature is not available.")
			return
		}
		c.Next()
	}
}

// Capabilities returns an account-bound display hint, never an authority token.
func (s *Service) Capabilities(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !validRead(c) {
		return
	}
	principal, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "Sign in to continue.")
		return
	}
	tenant, err := uuid.Parse(principal.TenantID)
	if err != nil || tenant == uuid.Nil {
		respond.Unauthenticated(c, "Sign in to continue.")
		return
	}
	if c.GetHeader("X-Helivanta-Expected-Tenant") != tenant.String() || c.GetHeader("X-Helivanta-Expected-Subject") != principal.Subject {
		respond.Conflict(c, "The current account changed. Reload before continuing.")
		return
	}
	snapshot, err := s.Snapshot(c.Request.Context(), tenant, principal.Subject)
	if err != nil {
		unavailable(c)
		return
	}
	respond.OK(c, snapshot)
}

// PublicCapabilities exposes no tenant, account or targeting information.
func (s *Service) PublicCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validRead(c) {
		return
	}
	snapshot, err := s.PublicSnapshot(c.Request.Context())
	if err != nil {
		unavailable(c)
		return
	}
	respond.OK(c, snapshot)
}

// PublicHandler shares the platform limiter before any tenant or session exists.
func (s *Service) PublicHandler(limiter ratelimit.Limiter) gin.HandlerFunc {
	if limiter == nil {
		panic("release availability requires rate limiter")
	}
	return func(c *gin.Context) {
		if !limiter.Allow("release-availability:"+c.ClientIP(), ratelimit.Rule{Rate: 60, Burst: 20, Per: time.Minute}, s.now()).Allowed {
			respond.Error(c, 429, "rate_limited", "Try again shortly.")
			return
		}
		s.PublicCapabilities(c)
	}
}

func validRead(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 {
		respond.Error(c, 400, "invalid_request", "This availability read takes no query or body.")
		return false
	}
	return true
}

func unavailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	respond.Error(c, http.StatusServiceUnavailable, "release_unavailable", "Feature availability could not be verified. Try again.")
}
