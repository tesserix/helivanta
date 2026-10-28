package globalapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// FromEnv requires explicit production destinations and proxy trust.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{Development: getenv("HELIVANTA_ENV") == "dev", Timeout: 30 * time.Second, TransportSecurity: getenv("GLOBAL_API_TRANSPORT_SECURITY")}
	raw := getenv("GLOBAL_API_UPSTREAMS")
	if raw == "" {
		if !cfg.Development {
			return cfg, errors.New("GLOBAL_API_UPSTREAMS is required outside development")
		}
		origin := getenv("GLOBAL_API_UPSTREAM_URL")
		if origin == "" {
			origin = "http://localhost:8080"
		}
		cfg.Upstreams = make(map[string]string)
		for _, owner := range owners {
			cfg.Upstreams[owner] = origin
		}
	} else if err := json.Unmarshal([]byte(raw), &cfg.Upstreams); err != nil {
		return cfg, errors.New("GLOBAL_API_UPSTREAMS must be a product-to-origin JSON object")
	}
	trust := getenv("GLOBAL_API_TRUSTED_PROXY_CIDRS")
	if trust == "" && !cfg.Development {
		return cfg, errors.New("GLOBAL_API_TRUSTED_PROXY_CIDRS must list proxy CIDRs or none")
	}
	if trust != "" && trust != "none" {
		for _, value := range strings.Split(trust, ",") {
			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, strings.TrimSpace(value))
		}
	}
	return cfg, nil
}
