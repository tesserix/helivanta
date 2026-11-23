package release

import (
	"context"

	"github.com/google/uuid"
)

// OperatingTemplateEnabled controls availability only; IAM still admits and reviews the exact duty.
func (s *Service) OperatingTemplateEnabled(ctx context.Context, tenant uuid.UUID, subject, template string) (bool, error) {
	key, known := operatingTemplateCapabilities[template]
	if !known {
		return false, ErrUnavailable
	}
	if key == "baseline" {
		return true, nil
	}
	if s == nil {
		return false, ErrUnavailable
	}
	snapshot, err := s.Snapshot(ctx, tenant, subject)
	if err != nil {
		return false, err
	}
	for _, row := range snapshot.Capabilities {
		if row.Key == key {
			return row.Available, nil
		}
	}
	return false, ErrUnavailable
}
