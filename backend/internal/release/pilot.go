package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
)

// PilotConfig narrows compiled pilot eligibility; it never enables provider flags.
type PilotConfig struct {
	TenantIDs []uuid.UUID `json:"tenant_ids"`
	ExpiresAt time.Time   `json:"expires_at"`
}

func copyPilot(p *PilotConfig, now time.Time) (*PilotConfig, error) {
	if p == nil {
		return nil, nil
	}
	if len(p.TenantIDs) == 0 || len(p.TenantIDs) > 20 || p.ExpiresAt.IsZero() || p.ExpiresAt.After(now.Add(7*24*time.Hour)) {
		return nil, errors.New("invalid release pilot policy")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range p.TenantIDs {
		if id == uuid.Nil || seen[id] {
			return nil, errors.New("invalid release pilot tenants")
		}
		seen[id] = true
	}
	return &PilotConfig{TenantIDs: append([]uuid.UUID(nil), p.TenantIDs...), ExpiresAt: p.ExpiresAt}, nil
}

func (s *Service) pilotAllows(tenant string, now time.Time) bool {
	if s.pilot == nil || !now.Before(s.pilot.ExpiresAt) {
		return false
	}
	for _, id := range s.pilot.TenantIDs {
		if id.String() == tenant {
			return true
		}
	}
	return false
}

func readPilotFile(path string) (*PilotConfig, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path) // #nosec G304 -- Operator-mounted policy, never request input.
	if err != nil {
		return nil, errors.New("release pilot policy unavailable")
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(data) > 8192 || !json.Valid(data) || !unambiguousJSON(data) {
		return nil, errors.New("invalid release pilot policy")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var policy PilotConfig
	if d.Decode(&policy) != nil {
		return nil, errors.New("invalid release pilot policy")
	}
	return &policy, nil
}
