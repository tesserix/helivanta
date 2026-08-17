package testutil

import (
	"testing"

	"github.com/tesserix/helivanta/internal/testinfra"
)

// StartNATS boots nats:2.10-alpine and returns the connection URL.
func StartNATS(t *testing.T) string {
	return testinfra.StartNATS(t)
}
