package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	gb "github.com/growthbook/growthbook-golang"
)

const (
	RefreshInterval  = 30 * time.Second
	SnapshotLifetime = time.Minute
	maxPayloadBytes  = 256 << 10
	flagPrefix       = "helivanta.release."
)

var ErrUnavailable = errors.New("release capabilities unavailable")

type Definition struct {
	Key          string
	Eligible     bool
	Dependencies []string
}

type Config struct {
	URL       string
	ClientKey string
	Client    *http.Client
	Now       func() time.Time
}

type providerSnapshot struct {
	loadedAt time.Time
	features *gb.Client
}

type Service struct {
	endpoint           string
	client             *http.Client
	now                func() time.Time
	current            atomic.Pointer[providerSnapshot]
	developmentFixture bool
}

// New creates a locally evaluated, bounded-lifetime GrowthBook release source.
func New(cfg Config) (*Service, error) {
	origin, err := url.Parse(cfg.URL)
	if err != nil || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errors.New("invalid release provider origin")
	}
	if origin.Scheme != "https" && (origin.Scheme != "http" || origin.Hostname() != "growthbook.growthbook.svc.cluster.local" || origin.Port() != "3100") {
		return nil, errors.New("release provider requires HTTPS or the declared in-cluster service")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(cfg.ClientKey) {
		return nil, errors.New("release provider reader key required")
	}
	client := http.Client{Timeout: 5 * time.Second}
	if cfg.Client != nil {
		client = *cfg.Client
	}
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	origin.Path = "/api/features/" + cfg.ClientKey
	return &Service{endpoint: origin.String(), client: &client, now: cfg.Now}, nil
}

// RouteCapability returns only an explicitly classified method and route pattern.
func RouteCapability(method, path string) (string, bool) {
	key, ok := routeCapabilities[method+" "+path]
	return key, ok
}

// Refresh atomically publishes a complete validated provider snapshot.
func (s *Service) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := s.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer func() {
		// The read outcome governs snapshot publication; closing a read-only body cannot change it.
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPayloadBytes+1))
	if err != nil || len(body) > maxPayloadBytes {
		return ErrUnavailable
	}
	features, err := validatePayload(body)
	if err != nil {
		return ErrUnavailable
	}
	client, err := gb.NewClient(ctx, gb.WithJsonFeatures(string(features)), gb.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		return ErrUnavailable
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	s.current.Store(&providerSnapshot{loadedAt: s.now().UTC(), features: client})
	return nil
}

// Run refreshes independently of business requests and exits with its owner.
func (s *Service) Run(ctx context.Context) {
	refresh := func() {
		if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "release provider refresh unavailable")
		}
	}
	refresh()
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func (s *Service) evaluate(ctx context.Context, tenant string) ([]ReleaseCapability, *providerSnapshot, error) {
	current := s.current.Load()
	now := s.now()
	if ctx.Err() != nil || current == nil || now.Before(current.loadedAt) || !now.Before(current.loadedAt.Add(SnapshotLifetime)) {
		return nil, nil, ErrUnavailable
	}
	attributes := gb.Attributes{}
	if tenant != "" {
		attributes["tenant_id"] = tenant
	}
	client, err := current.features.WithAttributes(attributes)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	resolved := map[string]bool{}
	var resolve func(string) (bool, error)
	resolve = func(key string) (bool, error) {
		if value, exists := resolved[key]; exists {
			return value, nil
		}
		var definition *Definition
		for i := range definitions {
			if definitions[i].Key == key {
				definition = &definitions[i]
				break
			}
		}
		if definition == nil {
			return false, ErrUnavailable
		}
		resolved[key] = false
		if !definition.Eligible && !s.developmentFixture {
			return false, nil
		}
		result := client.EvalFeature(ctx, flagPrefix+key)
		if result == nil || result.Value == nil {
			return false, nil
		}
		value, ok := result.Value.(bool)
		if !ok {
			return false, ErrUnavailable
		}
		if !value {
			return false, nil
		}
		for _, dependency := range definition.Dependencies {
			enabled, err := resolve(dependency)
			if err != nil {
				return false, err
			}
			if !enabled {
				return false, nil
			}
		}
		resolved[key] = true
		return true, nil
	}
	result := make([]ReleaseCapability, 0, len(definitions))
	for _, definition := range definitions {
		enabled, err := resolve(definition.Key)
		if err != nil {
			return nil, nil, err
		}
		result = append(result, ReleaseCapability{Key: definition.Key, Available: enabled})
	}
	return result, current, nil
}

// Snapshot binds effective capabilities to the authenticated tenant and account.
func (s *Service) Snapshot(ctx context.Context, tenant uuid.UUID, subject string) (ReleaseSnapshot, error) {
	if tenant == uuid.Nil || subject == "" || len(subject) > 200 || subject != strings.TrimSpace(subject) {
		return ReleaseSnapshot{}, ErrUnavailable
	}
	capabilities, current, err := s.evaluate(ctx, tenant.String())
	if err != nil {
		return ReleaseSnapshot{}, err
	}
	return ReleaseSnapshot{TenantID: tenant, Subject: subject, PolicyVersion: PolicyVersion, IssuedAt: current.loadedAt, ExpiresAt: current.loadedAt.Add(SnapshotLifetime), Capabilities: capabilities}, nil
}

// PublicSnapshot discloses only whether a new owner can begin onboarding.
func (s *Service) PublicSnapshot(ctx context.Context) (PublicReleaseSnapshot, error) {
	capabilities, current, err := s.evaluate(ctx, "")
	if err != nil {
		return PublicReleaseSnapshot{}, err
	}
	result := PublicReleaseSnapshot{PolicyVersion: PolicyVersion, IssuedAt: current.loadedAt, ExpiresAt: current.loadedAt.Add(SnapshotLifetime)}
	for _, capability := range capabilities {
		if capability.Key == "onboarding" {
			result.Onboarding = capability.Available
		}
	}
	return result, nil
}

func validatePayload(body []byte) ([]byte, error) {
	if !json.Valid(body) || !unambiguousJSON(body) {
		return nil, ErrUnavailable
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, ErrUnavailable
	}
	if encrypted := bytes.TrimSpace(payload["encryptedFeatures"]); len(encrypted) > 0 && !bytes.Equal(encrypted, []byte(`""`)) {
		return nil, ErrUnavailable
	}
	raw := bytes.TrimSpace(payload["features"])
	if len(raw) == 0 || raw[0] != '{' {
		return nil, ErrUnavailable
	}
	var features map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &features); err != nil || features == nil || len(features) > len(definitions) {
		return nil, ErrUnavailable
	}
	known := map[string]bool{}
	for _, definition := range definitions {
		known[flagPrefix+definition.Key] = true
	}
	for key, feature := range features {
		if !known[key] || !jsonBoolean(feature["defaultValue"]) {
			return nil, ErrUnavailable
		}
		for field := range feature {
			if field != "defaultValue" && field != "rules" {
				return nil, ErrUnavailable
			}
		}
		var rules []map[string]json.RawMessage
		if rawRules, exists := feature["rules"]; exists {
			if err := json.Unmarshal(rawRules, &rules); err != nil || len(rules) > 64 {
				return nil, ErrUnavailable
			}
		}
		for _, rule := range rules {
			if !jsonBoolean(rule["force"]) {
				return nil, ErrUnavailable
			}
			for field := range rule {
				if field != "force" && field != "condition" {
					return nil, ErrUnavailable
				}
			}
			if condition, exists := rule["condition"]; exists {
				condition = bytes.TrimSpace(condition)
				if len(condition) == 0 || condition[0] != '{' {
					return nil, ErrUnavailable
				}
			}
		}
	}
	return raw, nil
}

func unambiguousJSON(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var consume func(int) bool
	consume = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !consume(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !consume(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !consume(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func jsonBoolean(value []byte) bool {
	value = bytes.TrimSpace(value)
	return bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))
}
