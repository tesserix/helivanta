package workforce

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/requestid"
)

type postingPersistenceFailure struct {
	cause          error
	operation      string
	elapsed, total time.Duration
}

func (e *postingPersistenceFailure) Error() string { return "posting decision persistence failed" }
func (e *postingPersistenceFailure) Unwrap() error { return e.cause }

func enrichPostingPersistenceFailure(c *gin.Context, err error) {
	var failure *postingPersistenceFailure
	if errors.As(err, &failure) {
		requestid.Enrich(c, "persistence_operation", failure.operation,
			"persistence_operation_ms", failure.elapsed.Milliseconds(),
			"persistence_total_ms", failure.total.Milliseconds())
	}
}
