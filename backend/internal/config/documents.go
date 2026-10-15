package config

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// RequireDocumentStorage refuses uploads without a complete private pipeline.
func (c Config) RequireDocumentStorage() (bool, error) {
	if c.DocumentQuarantineBucket == "" && c.DocumentReleasedBucket == "" {
		return false, nil
	}
	host, port, err := net.SplitHostPort(c.TemporalAddress)
	number, parseErr := strconv.Atoi(port)
	if c.DocumentQuarantineBucket == "" || c.DocumentReleasedBucket == "" || c.DocumentQuarantineBucket == c.DocumentReleasedBucket || err != nil || host == "" || parseErr != nil || number < 1 || number > 65535 || strings.TrimSpace(c.TemporalNamespace) == "" {
		// Partial configuration must not accept files that cannot be durably processed.
		return false, errors.New("private document storage requires distinct buckets and a named Temporal namespace/address")
	}
	return true, nil
}

// RequireDocumentWorker requires its own database identity and private scanner.
func (c Config) RequireDocumentWorker() error {
	enabled, err := c.RequireDocumentStorage()
	if err != nil {
		return err
	}
	host, port, parseErr := net.SplitHostPort(c.ClamAVAddress)
	number, numberErr := strconv.Atoi(port)
	if !enabled || c.DocumentWorkerDatabaseURL == "" || parseErr != nil || host == "" || numberErr != nil || number < 1 || number > 65535 {
		return errors.New("document worker requires private storage, its runtime database credential and scanner address")
	}
	return nil
}
