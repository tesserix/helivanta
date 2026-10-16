package testinfra

import (
	"os"
	"strings"

	"github.com/google/uuid"
)

func init() {
	// A shared go-test parent can outlive a reaper between package runs.
	// Explicit IDs still allow the intentional shared-reaper regression.
	if os.Getenv("TESTCONTAINERS_SESSION_ID") == "" {
		if err := os.Setenv("TESTCONTAINERS_SESSION_ID", strings.ReplaceAll(uuid.NewString(), "-", "")); err != nil {
			panic(err)
		}
	}
}
