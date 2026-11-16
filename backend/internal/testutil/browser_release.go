package testutil

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/pkg/authn"
)

// MountBrowserReleaseCapabilities gives disposable owner fixtures the real bounded
// release evaluator without changing their authentication or domain authorization.
func MountBrowserReleaseCapabilities(t *testing.T, owner http.Handler, tokens StaticVerifier) {
	t.Helper()
	engine, ok := owner.(*gin.Engine)
	require.True(t, ok, "browser owner must be the authenticated fixture engine")
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/v1/workspace/release-capabilities" {
			return
		}
	}
	source, err := release.DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true")
	require.NoError(t, err)
	require.NoError(t, source.Refresh(t.Context()))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go source.Run(ctx)
	engine.GET("/v1/workspace/release-capabilities", authn.Middleware(tokens, neverRevoked{}), source.Capabilities)
}
