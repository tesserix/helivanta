package config

import (
	"errors"
	"path/filepath"
	"strings"
)

// RequireTransit reports whether staff identifiers can be encrypted; a partial configuration refuses boot.
func (c Config) RequireTransit() (bool, error) {
	values := []string{c.OpenBaoAddr, c.OpenBaoTransitMount, c.OpenBaoAuthRole, c.OpenBaoTokenPath}
	set := 0
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			set++
		}
	}
	if set == 0 {
		return false, nil
	}
	if set != len(values) || !filepath.IsAbs(c.OpenBaoTokenPath) {
		// Half a vault configuration would silently store nothing while the pod reports ready.
		return false, errors.New("openbao transit requires address, mount, auth role and an absolute token path")
	}
	return true, nil
}
