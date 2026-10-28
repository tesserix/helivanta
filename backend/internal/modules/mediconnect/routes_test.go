package mediconnect

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"testing"
)

type member struct{}

func (member) IsMember(context.Context, string, string) (bool, error) { return true, nil }

func TestAccountRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := platform.NewRouter(gin.New().Group("/v1"), member{})
	New().Routes(router, platform.Deps{})
	require.Len(t, router.Declared(), 17)
	require.Equal(t, "/v1/mediconnect/account", router.Declared()[0].Path)
}

func TestPreferencesRouteIsRegistered(t *testing.T) {
	router := platform.NewRouter(gin.New().Group("/v1"), member{})
	New().Routes(router, platform.Deps{})
	var found bool
	for _, route := range router.Declared() {
		if route.Method == "PATCH" && route.Path == "/v1/mediconnect/account/preferences" {
			found = true
		}
	}
	require.True(t, found)
}
