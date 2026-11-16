package release

import (
	"errors"
	"io"
	"os"
	"strings"
)

// FromFile reads the namespace-mounted SDK reader key without exposing it to logs.
func FromFile(origin, keyFile string) (*Service, error) {
	if keyFile == "" {
		return nil, errors.New("release provider reader key file required")
	}
	file, err := os.Open(keyFile) // #nosec G304 -- Operator-controlled startup configuration, never request input.
	if err != nil {
		return nil, errors.New("release provider reader key file unavailable")
	}
	defer func() {
		// No writes are buffered for this read-only mounted credential.
		_ = file.Close()
	}()
	key, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil || len(key) > 1024 {
		return nil, errors.New("invalid release provider reader key file")
	}
	return New(Config{URL: origin, ClientKey: strings.TrimSpace(string(key))})
}
