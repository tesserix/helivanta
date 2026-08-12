package respond_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform/respond"
)

func run(h gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/t", nil))
	return w
}

func TestSuccessHelpersPreserveShapes(t *testing.T) {
	w := run(func(c *gin.Context) { respond.OK(c, gin.H{"data": []string{"a"}}) })
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"data":["a"]}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Accepted(c, gin.H{"id": "x"}) })
	require.Equal(t, http.StatusAccepted, w.Code)
	require.JSONEq(t, `{"id":"x"}`, w.Body.String())
}

func TestErrorHelpers(t *testing.T) {
	w := run(func(c *gin.Context) { respond.NotFound(c, "ping") })
	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"not_found","message":"ping not found"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Conflict(c, "already dispensed") })
	require.Equal(t, http.StatusConflict, w.Code)
	require.JSONEq(t, `{"error":"conflict","message":"already dispensed"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.BadRequest(c, errors.New("bad field")) })
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_request","message":"bad field"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.InternalErr(c, errors.New("insert failed"), "could not record ping") })
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"error":"internal","message":"could not record ping"}`, w.Body.String())
}

func TestForbiddenEnvelope(t *testing.T) {
	w := run(func(c *gin.Context) { respond.Forbidden(c, "missing permission") })

	require.Equal(t, http.StatusForbidden, w.Code)
	require.JSONEq(t, `{"error":"forbidden","message":"missing permission"}`, w.Body.String())
}
