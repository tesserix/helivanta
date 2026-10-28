package workforce

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// maxAcknowledgedClashes matches the contract's acknowledged_clashes bound; tests lower it.
var maxAcknowledgedClashes = 2000

// rosterClashJoin pairs current allocations with reviewed credentials not valid on the roster date.
const rosterClashJoin = `workforce_roster_allocations a
 JOIN workforce_roster_credential_standing c ON (c.tenant_id,c.hospital_id,c.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id) AND (a.roster_date>c.valid_until OR a.roster_date<c.valid_from)`

const clashStandingCase = `CASE WHEN a.roster_date>c.valid_until THEN 'expired' ELSE 'not_yet_valid' END`

func (r rosterCredentialClash) PageKey() (time.Time, uuid.UUID) {
	day, _ := time.Parse(time.DateOnly, r.RosterDate)
	return day, r.AllocationID
}

// clashRow scans the aggregated credentials before they decode into the contract type.
type clashRow struct {
	AllocationID, StaffID                 uuid.UUID
	EmployeeCode, DisplayName, RosterDate string
	Credentials                           []byte
}

// credentialClashAck is one allocation-credential pair recorded when a roster is frozen over it.
type credentialClashAck struct {
	AllocationID    uuid.UUID
	StaffID         uuid.UUID
	CredentialID    uuid.UUID
	ValidityVersion int64
	Standing        string
}

type credentialClashesOutstanding struct{ outstanding int }

func (e credentialClashesOutstanding) Error() string {
	return fmt.Sprintf("%d credential clashes are not acknowledged", e.outstanding)
}

var (
	errTooManyClashes      = &rosterProblem{409, "too_many_clashes", "This roster has more than 2,000 credential clashes. Fix the allocations before freezing it."}
	errClashAckNotFreezing = &rosterProblem{400, "invalid_request", "Only freezing a roster acknowledges credential clashes."}
)

func (h *handlers) rosterCredentialClashes(c *gin.Context, params pagination.Params) ([]rosterCredentialClash, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := rosterID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	var scanned []clashRow
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, true); err != nil {
			return err
		}
		if _, err := loadRoster(tx, p, hospital, id); err != nil {
			return err
		}
		q := tx.Table(rosterClashJoin+` JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)`).
			Select(`a.id AS allocation_id,a.staff_id,s.employee_code,s.display_name,a.roster_date::text AS roster_date,
 jsonb_agg(jsonb_build_object('credential_id',c.credential_id,'type',c.type,'valid_from',c.valid_from::text,'valid_until',c.valid_until::text,'validity_version',c.version,'standing',`+clashStandingCase+`) ORDER BY c.type,c.credential_id) AS credentials`).
			Where("a.tenant_id=? AND a.hospital_id=? AND a.roster_id=? AND a.removed_version IS NULL", p.TenantID, hospital, id).
			Group("a.id,a.staff_id,s.employee_code,s.display_name,a.roster_date").Order("a.roster_date,a.id").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(a.roster_date,a.id) > (?::date,?)", params.After.CreatedAt.UTC().Format(time.DateOnly), params.After.ID)
		}
		if err := q.Find(&scanned).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.roster_credential_clashes.read',?,'{}'::jsonb)`,
			p.TenantID, hospital, p.Subject, id).Error
	})
	if err != nil {
		respondRosterError(c, err)
		return nil, nil
	}
	rows := make([]rosterCredentialClash, len(scanned))
	for i, row := range scanned {
		rows[i] = rosterCredentialClash{AllocationID: row.AllocationID, StaffID: row.StaffID, EmployeeCode: row.EmployeeCode, DisplayName: row.DisplayName, RosterDate: row.RosterDate}
		if err := json.Unmarshal(row.Credentials, &rows[i].Credentials); err != nil {
			respondRosterError(c, err)
			return nil, nil
		}
	}
	return rows, nil
}

// checkCredentialClashes returns the roster's current clashes, refusing any allocation not acknowledged.
func checkCredentialClashes(tx *gorm.DB, p authn.Principal, hospital, roster uuid.UUID, acknowledged []uuid.UUID) ([]credentialClashAck, error) {
	var clashes []credentialClashAck
	if err := tx.Table(rosterClashJoin).
		Select(`a.id AS allocation_id,a.staff_id,c.credential_id,c.version AS validity_version,`+clashStandingCase+` AS standing`).
		Where("a.tenant_id=? AND a.hospital_id=? AND a.roster_id=? AND a.removed_version IS NULL", p.TenantID, hospital, roster).
		Order("a.roster_date,a.id,c.credential_id").Limit(maxAcknowledgedClashes + 1).Find(&clashes).Error; err != nil {
		return nil, err
	}
	if len(clashes) > maxAcknowledgedClashes {
		return nil, errTooManyClashes
	}
	outstanding := map[uuid.UUID]bool{}
	for _, clash := range clashes {
		if !slices.Contains(acknowledged, clash.AllocationID) {
			outstanding[clash.AllocationID] = true
		}
	}
	if len(outstanding) > 0 {
		return nil, credentialClashesOutstanding{len(outstanding)}
	}
	return clashes, nil
}

func insertClashAcknowledgements(tx *gorm.DB, p authn.Principal, hospital, revision uuid.UUID, acks []credentialClashAck) error {
	for chunk := range slices.Chunk(acks, allocationInsertChunk) {
		rows, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_roster_credential_acknowledgements(tenant_id,hospital_id,revision_id,allocation_id,staff_id,credential_id,validity_version,standing,acknowledged_by_subject)
 SELECT ?,?,?,x."AllocationID",x."StaffID",x."CredentialID",x."ValidityVersion",x."Standing",?
 FROM jsonb_to_recordset(?::jsonb) AS x("AllocationID" uuid,"StaffID" uuid,"CredentialID" uuid,"ValidityVersion" bigint,"Standing" text)`,
			p.TenantID, hospital, revision, p.Subject, string(rows)).Error; err != nil {
			return err
		}
	}
	return nil
}
