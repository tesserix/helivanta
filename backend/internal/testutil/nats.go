package testutil

import (
	"testing"

	"github.com/tesserix/hms/internal/containerhelpers"
)

// StartNATS boots nats:2.10-alpine and returns the connection URL.
func StartNATS(t *testing.T) string {
	return containerhelpers.StartNATS(t)
}
