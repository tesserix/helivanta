// Package attachments supplies private object naming and content scanning.
package attachments

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ObjectScope identifies an attachment; UploadedAt is assigned by the server.
type ObjectScope struct {
	TenantID   uuid.UUID
	HospitalID uuid.UUID
	OwnerKind  string
	OwnerID    uuid.UUID
	RecordID   uuid.UUID
	FileID     uuid.UUID
	Version    int64
	UploadedAt time.Time
}

// ObjectKey constructs an immutable key from validated opaque identifiers.
func ObjectKey(scope ObjectScope) (string, error) {
	if scope.TenantID == uuid.Nil || scope.OwnerID == uuid.Nil || scope.RecordID == uuid.Nil || scope.FileID == uuid.Nil {
		return "", errors.New("document object scope requires all ownership identifiers")
	}
	switch scope.OwnerKind {
	case "patient", "staff":
		if scope.HospitalID == uuid.Nil {
			return "", errors.New("clinical document scope requires a hospital")
		}
	case "organisation", "practitioner":
		if scope.HospitalID != uuid.Nil {
			return "", errors.New("tenant document scope cannot include a hospital")
		}
	default:
		return "", errors.New("document object owner kind is unsupported")
	}
	if scope.Version < 1 || scope.UploadedAt.IsZero() || scope.UploadedAt.UTC().Year() < 1970 || scope.UploadedAt.UTC().Year() > 9999 {
		return "", errors.New("document object version and upload date are invalid")
	}
	if scope.OwnerKind == "organisation" || scope.OwnerKind == "practitioner" {
		return fmt.Sprintf("t/%s/o/%s/%s/r/%s/d/%s/f/%s/v/%d", scope.TenantID, scope.OwnerKind, scope.OwnerID, scope.RecordID, scope.UploadedAt.UTC().Format("2006/01/02"), scope.FileID, scope.Version), nil
	}
	return fmt.Sprintf("t/%s/h/%s/o/%s/%s/r/%s/d/%s/f/%s/v/%d", scope.TenantID, scope.HospitalID, scope.OwnerKind, scope.OwnerID, scope.RecordID, scope.UploadedAt.UTC().Format("2006/01/02"), scope.FileID, scope.Version), nil
}
