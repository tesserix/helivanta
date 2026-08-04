package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReadyCheck reports one dependency's readiness (name → error).
type ReadyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Server struct {
	Engine *gin.Engine
}

func New(readyChecks []ReadyCheck, middlewares ...gin.HandlerFunc) *Server {
	e := gin.New()
	e.Use(gin.Recovery())
	for _, mw := range middlewares {
		e.Use(mw)
	}
	e.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	e.GET("/readyz", func(c *gin.Context) {
		for _, rc := range readyChecks {
			if err := rc.Check(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready", "failed": rc.Name})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	return &Server{Engine: e}
}
