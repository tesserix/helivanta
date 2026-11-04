package workforce

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

var errLeaveRoutingInvalid = errors.New("leave routing policy is invalid")
var errLeaveRoutingVersion = errors.New("leave routing policy version is stale")

const leaveRoutingColumns = "version,mode,escalation_hours::int AS escalation_hours,source_reference,reviewed_by_subject,created_at"

func currentLeaveRouting(tx *gorm.DB, tenant string, hospital uuid.UUID) (leaveRoutingPolicy, error) {
	result := leaveRoutingPolicy{Mode: "hospital"}
	err := tx.Table("workforce_leave_routing_revisions").Select(leaveRoutingColumns).Where("tenant_id=? AND hospital_id=?", tenant, hospital).Order("version DESC").Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return leaveRoutingPolicy{Mode: "hospital"}, nil
	}
	return result, err
}

func (h *handlers) getLeaveRouting(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result leaveRoutingPolicy
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = currentLeaveRouting(tx, p.TenantID, hospital)
		return err
	})
	if err != nil {
		respondLeaveRoutingError(c, err)
		return
	}
	respond.OK(c, result)
}

// A new revision applies only to requests filed afterwards; filed requests keep the route pinned at filing.
func (h *handlers) putLeaveRouting(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in leaveRoutingPolicyInput
	if !decode(c, &in) {
		return
	}
	in.SourceReference = strings.TrimSpace(in.SourceReference)
	if !*in.Reviewed || in.SourceReference == "" || (in.Mode == "reporting_line") != (in.EscalationHours != nil) {
		respondLeaveRoutingError(c, errLeaveRoutingInvalid)
		return
	}
	var result leaveRoutingPolicy
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_routing", leaveLedgerLock); err != nil {
			return err
		}
		current, err := currentLeaveRouting(tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		if current.Version > 0 && current.Mode == in.Mode && current.SourceReference == in.SourceReference && equalHours(current.EscalationHours, in.EscalationHours) {
			result = current
			return nil
		}
		if current.Version != *in.ExpectedVersion {
			return errLeaveRoutingVersion
		}
		version := current.Version + 1
		if err := tx.Exec(`INSERT INTO workforce_leave_routing_revisions(tenant_id,hospital_id,version,mode,escalation_hours,source_reference,reviewed_by_subject) VALUES(?,?,?,?,?,?,?)`, p.TenantID, hospital, version, in.Mode, in.EscalationHours, in.SourceReference, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_routing.changed',?,jsonb_build_object('version',?::int,'previous_version',?::int,'mode',?::text,'escalation_hours',?::int))`, p.TenantID, hospital, p.Subject, hospital, version, current.Version, in.Mode, in.EscalationHours).Error; err != nil {
			return err
		}
		result, err = currentLeaveRouting(tx, p.TenantID, hospital)
		return err
	})
	if err != nil {
		respondLeaveRoutingError(c, err)
		return
	}
	respond.OK(c, result)
}

func equalHours(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func respondLeaveRoutingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errLeaveRoutingInvalid):
		respond.Error(c, 400, "invalid_request", "Review the policy and give its source reference. Reporting-line routing needs an escalation interval of 1 to 720 hours; hospital routing takes none.")
	case errors.Is(err, errLeaveRoutingVersion):
		respond.Conflict(c, "The leave routing policy changed since you opened it. Reload before saving.")
	default:
		respondLeaveLedgerError(c, err)
	}
}

const noEligibleReportingLine = "no_eligible_reporting_line"

// loadLeaveRouting reads the route pinned at filing; requests filed before any policy carry none.
func loadLeaveRouting(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (*leaveRequestRouting, error) {
	var pin struct {
		RoutingVersion *int
		Routed         bool
	}
	if err := tx.Table("workforce_leave_requests").Select("routing_version,routed").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&pin).Error; err != nil {
		return nil, err
	}
	if pin.RoutingVersion == nil {
		return nil, nil
	}
	routing := &leaveRequestRouting{Version: *pin.RoutingVersion, Routed: pin.Routed, Steps: []leaveRouteStep{}}
	if err := tx.Raw(`SELECT s.level,s.staff_id,st.employee_code,st.display_name,s.opens_at,s.opens_at<=now() AS open
 FROM workforce_leave_route_steps s JOIN workforce_staff st ON (st.tenant_id,st.hospital_id,st.id)=(s.tenant_id,s.hospital_id,s.staff_id)
 WHERE s.tenant_id=? AND s.hospital_id=? AND s.request_id=? ORDER BY s.level`, tenant, hospital, id).Scan(&routing.Steps).Error; err != nil {
		return nil, err
	}
	if routing.Routed && len(routing.Steps) == 0 {
		fallback := noEligibleReportingLine
		routing.Fallback = &fallback
	}
	return routing, nil
}

// deskRouteEvidence records that the leave desk, not a route step, decided a routed request.
func deskRouteEvidence(tx *gorm.DB, p authn.Principal, hospital, staffID, requestID uuid.UUID) ([]byte, error) {
	var pin struct {
		RoutingVersion *int
		Routed         bool
	}
	if err := tx.Table("workforce_leave_requests").Select("routing_version,routed").Where("tenant_id=? AND hospital_id=? AND id=? AND staff_id=?", p.TenantID, hospital, requestID, staffID).Scan(&pin).Error; err != nil {
		return nil, err
	}
	if !pin.Routed {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any{"route_level": "desk", "routing_version": *pin.RoutingVersion})
}
