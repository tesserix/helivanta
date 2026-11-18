package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClearPasswordRecoveryExpiresBothCapabilitiesAndPreservesOtherCookies(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	http.SetCookie(recorder, &http.Cookie{Name: "unrelated", Value: "preserved", Path: "/"})
	ClearPasswordRecovery(c)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 3)
	require.Equal(t, "unrelated", cookies[0].Name)
	require.Equal(t, "preserved", cookies[0].Value)
	for i, name := range []string{PasswordRecoveryCookie, PasswordRecoveryCommandCookie} {
		cookie := cookies[i+1]
		require.Equal(t, name, cookie.Name)
		require.Empty(t, cookie.Value)
		require.Equal(t, -1, cookie.MaxAge)
		require.Equal(t, "/", cookie.Path)
		require.Empty(t, cookie.Domain)
		require.True(t, cookie.Secure)
		require.True(t, cookie.HttpOnly)
		require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	}
}
