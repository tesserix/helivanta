package mediconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
)

// ClaimWorkloadVerifier authenticates the configured audience-bound owner workload.
type ClaimWorkloadVerifier interface {
	Verify(context.Context, string) error
	Subject() string
}

func validClaimCheck(in mediconnectcontract.ClaimCheckRequest) bool {
	for _, raw := range []string{in.TenantID, in.RequestID, in.HospitalID, in.PatientID} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			return false
		}
	}
	id, _ := uuid.Parse(in.RequestID)
	return (in.Relationship == "" || in.Relationship == "self" || in.Relationship == "child" || in.Relationship == "ward") && id.Version() == 7 && id.Variant() == uuid.RFC4122 && len(in.Subject) > 0 && len(in.Subject) <= 512 && strings.TrimSpace(in.Subject) == in.Subject && !strings.ContainsRune(in.Subject, 0)
}

// NewClaimCheckHandler observes exact relationship-bound claim state for a separately authenticated owner.
func NewClaimCheckHandler(deps platform.Deps, caller ClaimWorkloadVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		header := mediconnectcontract.ClaimWorkloadHeader

		if caller == nil {
			respond.Error(c, 503, "claim_check_unavailable", "Current claim verification unavailable")
			return
		}
		if len(c.Request.Header.Values(header)) != 1 || len(c.Request.Header.Values("Authorization")) != 0 {
			respond.Unauthenticated(c, "owner workload credential required")
			return
		}
		if verificationErr := caller.Verify(ctx, c.GetHeader(header)); verificationErr != nil {
			if errors.Is(verificationErr, workloadidentity.ErrRejected) {
				respond.Unauthenticated(c, "owner workload credential required")
			} else {
				respond.Error(c, 503, "claim_check_unavailable", "Current claim verification unavailable")
			}
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		if err != nil || !utf8.Valid(raw) {
			respond.BadRequest(c, errors.New("invalid claim check"))
			return
		}
		var in mediconnectcontract.ClaimCheckRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF || !validClaimCheck(in) {
			respond.BadRequest(c, errors.New("invalid claim check"))
			return
		}
		if deps.DB == nil {
			respond.Error(c, 503, "claim_check_unavailable", "Current claim verification unavailable")
			return
		}
		if in.Relationship == "" {
			in.Relationship = "self"
		}
		var result mediconnectcontract.ClaimCheckResponse
		err = deps.DB.WithTenant(ctx, in.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','system:mediconnect-current-claim',true),set_config('app.mediconnect_claim_id',?,true),set_config('app.mediconnect_claim_subject',?,true),set_config('app.mediconnect_claim_hospital',?,true),set_config('app.mediconnect_claim_patient',?,true)`, in.RequestID, in.Subject, in.HospitalID, in.PatientID).Error; err != nil {
				return err
			}
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM mediconnect_link_requests WHERE tenant_id=? AND id=? AND subject=? AND hospital_id=? AND patient_id=? AND relationship=? AND status='pending_verification' AND withdrawn_at IS NULL)`, in.TenantID, in.RequestID, in.Subject, in.HospitalID, in.PatientID, in.Relationship).Scan(&result.Current).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'current_claim_checked','mediconnect_link_requests',?)`, in.TenantID, caller.Subject(), in.RequestID).Error
		})
		if err != nil {
			respond.Error(c, 503, "claim_check_unavailable", "Current claim verification unavailable")
			return
		}
		respond.OK(c, result)
	}
}
