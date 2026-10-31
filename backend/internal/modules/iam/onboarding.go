package iam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

type WorkloadVerifier interface {
	Verify(context.Context, string) error
}

var onboardingID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
var errExistingMembership = errors.New("existing tenant requires its current administrator")

const hospitalProvisioningActor = "system:serviceaccount:onboarding:onboarding-api"

type onboardingInput struct {
	AllowBootstrap     bool   `json:"allowBootstrap"`
	TenantID           string `json:"tenantId"`
	OrganizationID     string `json:"organizationId"`
	InitialAdminUserID string `json:"initialAdminUserId"`
}

// NewOnboardingHandler accepts only the separately authenticated platform provisioner.
func NewOnboardingHandler(deps platform.Deps, caller WorkloadVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Header("Cache-Control", "no-store")
		credential := c.GetHeader("X-Onboarding-Token")
		authorization := c.GetHeader("Authorization")
		if len(c.Request.Header.Values("X-Onboarding-Token")) > 1 || len(c.Request.Header.Values("Authorization")) > 1 || (credential != "" && authorization != "") {
			respond.Unauthenticated(c, "ambiguous provisioning credentials")
			return
		}
		if credential == "" {
			credential = authorization
		}
		if caller == nil || caller.Verify(c.Request.Context(), credential) != nil {
			respond.Unauthenticated(c, "provisioning credential required")
			return
		}
		var in onboardingInput
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
			respond.Error(c, 400, "invalid_request", "invalid hospital binding")
			return
		}
		tenant, err := uuid.Parse(in.TenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != in.TenantID || !onboardingID.MatchString(in.OrganizationID) || !onboardingID.MatchString(in.InitialAdminUserID) {
			respond.Error(c, 400, "invalid_request", "invalid hospital binding")
			return
		}
		if deps.DB == nil || deps.Bus == nil || deps.Authz == nil || deps.Reconcile == nil {
			respond.Error(c, 503, "onboarding_pending", "hospital provisioning unavailable")
			return
		}
		err = deps.DB.WithTenant(c.Request.Context(), in.TenantID, func(tx *gorm.DB) error {
			if e := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "hospital-onboarding:"+in.TenantID).Error; e != nil {
				return e
			}
			var counts struct {
				Total int64
				Admin int64
			}
			if e := tx.Raw(`SELECT count(*) AS total,count(*) FILTER (WHERE subject=? AND role_key='tenant_admin') AS admin FROM iam_members WHERE tenant_id=?`, in.InitialAdminUserID, in.TenantID).Scan(&counts).Error; e != nil {
				return e
			}
			recordMembership := func() error {
				return tx.Exec(`INSERT INTO audit_events (tenant_id,actor_subject,action,resource_type,resource_id)
					SELECT ?,?,'membership_verified','hospital_onboarding',?
					WHERE NOT EXISTS (SELECT 1 FROM audit_events WHERE tenant_id=? AND resource_type='hospital_onboarding' AND resource_id=? AND action='membership_verified')`, in.TenantID, hospitalProvisioningActor, in.TenantID, in.TenantID, in.TenantID).Error
			}
			if counts.Total > 0 {
				if counts.Admin == 0 {
					return errExistingMembership
				}
				return recordMembership()
			}
			if !in.AllowBootstrap {
				return errExistingMembership
			}
			var bootstrapped bool
			if e := tx.Raw(`SELECT EXISTS(SELECT 1 FROM audit_events WHERE tenant_id=? AND resource_type='hospital_onboarding' AND resource_id=? AND action='membership_verified')`, in.TenantID, in.TenantID).Scan(&bootstrapped).Error; e != nil {
				return e
			}
			if bootstrapped {
				return errExistingMembership
			}
			if e := tx.Create(&member{TenantID: tenant, Subject: in.InitialAdminUserID, RoleKey: string(authz.RoleTenantAdmin)}).Error; e != nil {
				return e
			}
			if e := recordMembership(); e != nil {
				return e
			}
			data, e := json.Marshal(iamcontract.MemberChangedData{Subject: in.InitialAdminUserID, RoleKey: string(authz.RoleTenantAdmin)})
			if e != nil {
				return e
			}
			return deps.Bus.Publish(tx, iamcontract.SubjectMemberGranted, events.Event{Type: "MemberGranted", Version: 1, TenantID: in.TenantID, Data: data})
		})
		if errors.Is(err, errExistingMembership) {
			respond.Error(c, 409, "existing_membership", "use the existing hospital administrator")
			return
		}
		if err != nil {
			respond.Error(c, 503, "onboarding_pending", "hospital provisioning pending")
			return
		}
		// DB is committed before external authorization writes. Replay repairs a failed handoff.
		projection := &fgaSyncHandlers{db: deps.DB, authz: deps.Authz, reconcile: deps.Reconcile}
		data, marshalErr := json.Marshal(iamcontract.MemberChangedData{Subject: in.InitialAdminUserID, RoleKey: string(authz.RoleTenantAdmin)})
		if marshalErr != nil {
			respond.Error(c, 503, "onboarding_pending", "hospital authorization pending")
			return
		}
		err = projection.project(c.Request.Context(), events.Event{Type: "MemberGranted", Version: 1, TenantID: in.TenantID, Data: data})
		if err != nil {
			respond.Error(c, 503, "onboarding_pending", "hospital authorization pending")
			return
		}
		slog.InfoContext(c.Request.Context(), "hospital onboarding verified", "tenant_id", in.TenantID, "organization_id", in.OrganizationID, "actor", hospitalProvisioningActor)
		c.JSON(200, gin.H{"tenantId": in.TenantID, "organizationId": in.OrganizationID, "state": "complete"})
	}
}
