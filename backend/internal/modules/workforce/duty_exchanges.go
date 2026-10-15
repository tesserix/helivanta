package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (e dutyExchange) PageKey() (time.Time, uuid.UUID)      { return e.CreatedAt, e.ID }
func (e dutyExchangeEvent) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

// dutyLock serialises every exchange write in a hospital, so receiver checks see each other's effects.
const dutyLock = "duty-exchanges"

var (
	errDutyInvalid       = &rosterProblem{400, "invalid_request", "Choose swap or cover, name a counterpart allocation for swaps only, confirm the counterpart's consent and give a reason of 1 to 500 characters."}
	errDutyNoteRequired  = &rosterProblem{400, "invalid_request", "Rejecting or revoking an exchange needs a note."}
	errDutyReference     = &rosterProblem{400, "invalid_reference", "Choose current primary allocations in frozen rosters of this hospital, each held by the person named, for two different people."}
	errDutyReused        = &rosterProblem{409, "exchange_reused", "This exchange id was already used with different values."}
	errDutyStale         = &rosterProblem{409, "stale_version", "The exchange changed since you opened it. Reload before acting."}
	errDutyState         = &rosterProblem{409, "exchange_state", "The exchange is no longer in a state that allows this action."}
	errDutyHeld          = &rosterProblem{409, "allocation_in_exchange", "One of these allocations is already part of a pending or approved exchange."}
	errDutyStarted       = &rosterProblem{409, "exchange_started", "One of these duties has already started; exchanges cover future duty only."}
	errDutyRosterChanged = &rosterProblem{409, "roster_changed", "The exchanged duty is no longer current in a frozen roster. Reload the exchange."}
	errDutyPrimaryTaken  = &rosterProblem{409, "primary_taken", "The person receiving this duty already has a primary allocation on that date."}
	errDutyOnLeave       = &rosterProblem{409, "on_leave", "The person receiving this duty has approved full-day leave on that date."}
	errDutyOrphan        = &rosterProblem{409, "orphan_additional", "The person giving this duty would keep only additional allocations on that date."}
	errDutyStaffInactive = &rosterProblem{409, "staff_inactive", "Both people must be active staff."}
	errDutyPending       = &rosterProblem{409, "pending_exchanges", "Decide or cancel the pending duty exchanges on this roster first."}
	errDutySelfDecision  = errors.New("duty exchanges are decided by someone other than the filer")
)

const dutyExchangeView = `(SELECT e.tenant_id,e.hospital_id,e.id,e.kind,e.requester_staff_id,rs.employee_code AS requester_employee_code,rs.display_name AS requester_display_name,
 e.counterpart_staff_id,cs.employee_code AS counterpart_employee_code,cs.display_name AS counterpart_display_name,e.requester_allocation_id,e.counterpart_allocation_id,
 e.requester_replacement_id,e.counterpart_replacement_id,e.requester_restored_id,e.counterpart_restored_id,e.duty_from::text AS duty_from,e.duty_to::text AS duty_to,
 e.counterpart_consent,e.reason,e.status,e.requested_by_subject,e.decided_by_subject,e.decided_at,e.revoked_by_subject,e.version,e.created_at,e.updated_at
 FROM workforce_duty_exchanges e JOIN workforce_staff rs ON (rs.tenant_id,rs.hospital_id,rs.id)=(e.tenant_id,e.hospital_id,e.requester_staff_id)
 JOIN workforce_staff cs ON (cs.tenant_id,cs.hospital_id,cs.id)=(e.tenant_id,e.hospital_id,e.counterpart_staff_id)) AS v`
const dutyExchangeColumns = "id,kind,requester_staff_id,requester_employee_code,requester_display_name,counterpart_staff_id,counterpart_employee_code,counterpart_display_name,requester_allocation_id,counterpart_allocation_id,requester_replacement_id,counterpart_replacement_id,requester_restored_id,counterpart_restored_id,duty_from,duty_to,counterpart_consent,reason,status,requested_by_subject,decided_by_subject,decided_at,revoked_by_subject,version,created_at,updated_at"

type dutyExchangeRow struct {
	Kind                     string
	RequesterStaffID         uuid.UUID
	CounterpartStaffID       uuid.UUID
	RequesterAllocationID    uuid.UUID
	CounterpartAllocationID  *uuid.UUID
	RequesterReplacementID   *uuid.UUID
	CounterpartReplacementID *uuid.UUID
	Reason                   string
	Status                   string
	RequestedBySubject       string
	Version                  int64
}

// dutyMove hands one allocation from one person to another.
type dutyMove struct {
	allocation, from, to uuid.UUID
}

type dutySlot struct {
	ID           uuid.UUID
	RosterID     uuid.UUID
	StaffID      uuid.UUID
	RosterDate   string
	StartsAt     time.Time
	EndsAt       time.Time
	Additional   bool
	Active       bool
	RosterStatus string
	Future       bool
}

func dutyExchangeID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("exchange"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "duty exchange")
		return uuid.Nil, false
	}
	return id, true
}

func dutyReader(c *gin.Context) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && (set.Has(PermDutyRequest) || set.Has(PermDutyApprove))
}

// exchangeMoves lists the handovers an exchange makes; reversing hands the replacements back.
func exchangeMoves(r dutyExchangeRow, reverse bool) []dutyMove {
	if reverse {
		moves := []dutyMove{{*r.RequesterReplacementID, r.CounterpartStaffID, r.RequesterStaffID}}
		if r.Kind == "swap" {
			moves = append(moves, dutyMove{*r.CounterpartReplacementID, r.RequesterStaffID, r.CounterpartStaffID})
		}
		return moves
	}
	moves := []dutyMove{{r.RequesterAllocationID, r.RequesterStaffID, r.CounterpartStaffID}}
	if r.Kind == "swap" {
		moves = append(moves, dutyMove{*r.CounterpartAllocationID, r.CounterpartStaffID, r.RequesterStaffID})
	}
	return moves
}

func moveIDs(moves []dutyMove) []uuid.UUID {
	ids := make([]uuid.UUID, len(moves))
	for i, m := range moves {
		ids[i] = m.allocation
	}
	return ids
}

// lockDutyRosters takes each affected roster's write lock in id order, the same lock roster edits and transitions hold.
func lockDutyRosters(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, allocations []uuid.UUID) error {
	var rosters []uuid.UUID
	if err := tx.Raw(`SELECT DISTINCT roster_id FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND id IN ? ORDER BY roster_id`, p.TenantID, hospital, allocations).Scan(&rosters).Error; err != nil {
		return err
	}
	for _, roster := range rosters {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":roster:"+roster.String()).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`SELECT 1 FROM workforce_rosters WHERE tenant_id=? AND hospital_id=? AND id IN ? ORDER BY id FOR UPDATE`, p.TenantID, hospital, slices.Concat(rosters, []uuid.UUID{uuid.Nil})).Error
}

// checkDutyMoves applies the exchange rules to a set of handovers; the database constraints remain the backstop.
func checkDutyMoves(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, moves []dutyMove, filing bool) (map[uuid.UUID]dutySlot, error) {
	ids := moveIDs(moves)
	var rows []dutySlot
	if err := tx.Raw(`SELECT a.id,a.roster_id,a.staff_id,a.roster_date::text AS roster_date,a.starts_at,a.ends_at,a.additional,a.removed_version IS NULL AS active,r.status AS roster_status,a.starts_at > clock_timestamp() AS future
 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.id IN ?`, p.TenantID, hospital, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	slots := map[uuid.UUID]dutySlot{}
	for _, s := range rows {
		slots[s.ID] = s
	}
	unusable := errDutyRosterChanged
	if filing {
		unusable = errDutyReference
	}
	for _, m := range moves {
		s, ok := slots[m.allocation]
		if !ok || s.StaffID != m.from || s.Additional || !s.Active || s.RosterStatus != "frozen" {
			return nil, unusable
		}
		if !s.Future {
			return nil, errDutyStarted
		}
	}
	people := []uuid.UUID{moves[0].from, moves[0].to}
	var staff []struct {
		ID               uuid.UUID
		EmploymentStatus string
	}
	if err := tx.Raw(`SELECT id,employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id IN ? ORDER BY id FOR SHARE`, p.TenantID, hospital, people).Scan(&staff).Error; err != nil {
		return nil, err
	}
	if len(staff) != len(people) {
		return nil, errDutyReference
	}
	for _, s := range staff {
		if s.EmploymentStatus != "active" {
			return nil, errDutyStaffInactive
		}
	}
	outgoing := slices.Concat(ids, []uuid.UUID{uuid.Nil})
	for _, m := range moves {
		s := slots[m.allocation]
		var overlapping []uuid.UUID
		if err := tx.Raw(`SELECT id FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND removed_version IS NULL AND id NOT IN ?
 AND tstzrange(starts_at,ends_at,'[)') && tstzrange(?,?,'[)') ORDER BY id`, p.TenantID, hospital, m.to, outgoing, s.StartsAt, s.EndsAt).Scan(&overlapping).Error; err != nil {
			return nil, err
		}
		if len(overlapping) > 0 {
			return nil, allocationOverlap{ids: overlapping}
		}
		var taken, away bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND roster_date=?::date AND removed_version IS NULL AND NOT additional AND id NOT IN ?) AS taken,
 EXISTS(SELECT 1 FROM workforce_leave_requests r JOIN workforce_leave_request_days d ON (d.tenant_id,d.hospital_id,d.request_id)=(r.tenant_id,r.hospital_id,r.id)
  WHERE r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.status='approved' AND r.unit='full_day' AND d.day=?::date) AS away`,
			p.TenantID, hospital, m.to, s.RosterDate, outgoing, p.TenantID, hospital, m.to, s.RosterDate).Row().Scan(&taken, &away); err != nil {
			return nil, err
		}
		if taken {
			return nil, errDutyPrimaryTaken
		}
		if away {
			return nil, errDutyOnLeave
		}
	}
	for _, m := range moves {
		s := slots[m.allocation]
		incoming := 0
		for _, other := range moves {
			if other.to == m.from && slots[other.allocation].RosterDate == s.RosterDate {
				incoming++
			}
		}
		var orphaned bool
		if err := tx.Raw(`SELECT ?::int + (SELECT count(*) FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND roster_date=?::date AND removed_version IS NULL AND NOT additional AND id NOT IN ?) = 0
 AND EXISTS(SELECT 1 FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND roster_date=?::date AND removed_version IS NULL AND additional)`,
			incoming, p.TenantID, hospital, m.from, s.RosterDate, outgoing, p.TenantID, hospital, m.from, s.RosterDate).Row().Scan(&orphaned); err != nil {
			return nil, err
		}
		if orphaned {
			return nil, errDutyOrphan
		}
	}
	return slots, nil
}

// applyDutyMoves writes one `exchanged` revision per roster: every handed-over allocation is removed and an identical one added for its receiver.
func (h *handlers) applyDutyMoves(tx *gorm.DB, p authn.Principal, hospital, exchange uuid.UUID, moves []dutyMove, slots map[uuid.UUID]dutySlot) (map[uuid.UUID]uuid.UUID, error) {
	byRoster := map[uuid.UUID][]dutyMove{}
	rosters := []uuid.UUID{}
	for _, m := range moves {
		roster := slots[m.allocation].RosterID
		if _, seen := byRoster[roster]; !seen {
			rosters = append(rosters, roster)
		}
		byRoster[roster] = append(byRoster[roster], m)
	}
	slices.SortFunc(rosters, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	type written struct {
		roster, revision uuid.UUID
		version          int64
		outcome          rosterOutcome
	}
	writes := make([]written, 0, len(rosters))
	// Every removal precedes every insert, so a receiver never briefly holds both sides of a swap.
	for _, roster := range rosters {
		var version int64
		if err := tx.Raw(`SELECT version FROM workforce_rosters WHERE tenant_id=? AND hospital_id=? AND id=? AND status='frozen'`, p.TenantID, hospital, roster).Scan(&version).Error; err != nil {
			return nil, err
		}
		if version == 0 {
			return nil, errDutyRosterChanged
		}
		w := written{roster: roster, revision: uuid.New(), version: version + 1, outcome: rosterOutcome{action: "exchanged", status: "frozen", exchange: &exchange}}
		for _, m := range byRoster[roster] {
			w.outcome.remove = append(w.outcome.remove, m.allocation)
			w.outcome.add = append(w.outcome.add, plannedAllocation{ID: uuid.New(), StaffID: m.to})
		}
		if err := insertRosterRevision(tx, p, hospital, roster, w.revision, w.version, w.outcome); err != nil {
			return nil, err
		}
		if err := tx.Exec(`UPDATE workforce_rosters SET version=?,revision_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=?`, w.version, w.revision, p.TenantID, hospital, roster).Error; err != nil {
			return nil, err
		}
		removal := tx.Exec(`UPDATE workforce_roster_allocations SET removed_version=?,removed_revision_id=? WHERE tenant_id=? AND hospital_id=? AND roster_id=? AND removed_version IS NULL AND id IN ?`,
			w.version, w.revision, p.TenantID, hospital, roster, w.outcome.remove)
		if removal.Error != nil {
			return nil, removal.Error
		}
		if removal.RowsAffected != int64(len(w.outcome.remove)) {
			return nil, errDutyRosterChanged
		}
		writes = append(writes, w)
	}
	replacements := map[uuid.UUID]uuid.UUID{}
	for _, w := range writes {
		for i, m := range byRoster[w.roster] {
			replacement := w.outcome.add[i].ID
			if err := tx.Exec(`INSERT INTO workforce_roster_allocations(tenant_id,hospital_id,id,roster_id,staff_id,roster_date,shift_revision_id,area_revision_id,starts_at,ends_at,additional,added_version,added_revision_id)
 SELECT tenant_id,hospital_id,?,roster_id,?,roster_date,shift_revision_id,area_revision_id,starts_at,ends_at,false,?,? FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND id=?`,
				replacement, m.to, w.version, w.revision, p.TenantID, hospital, m.allocation).Error; err != nil {
				return nil, err
			}
			replacements[m.allocation] = replacement
		}
	}
	for _, w := range writes {
		if err := h.recordRosterChange(tx, p, hospital, w.roster, w.revision, w.version, w.outcome); err != nil {
			return nil, err
		}
	}
	return replacements, nil
}

// refusePendingExchanges keeps a roster frozen while any of its allocations wait for an exchange decision.
func refusePendingExchanges(tx *gorm.DB, p authn.Principal, hospital, roster uuid.UUID) error {
	var pending bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_duty_exchange_holds x JOIN workforce_duty_exchanges e ON (e.tenant_id,e.hospital_id,e.id)=(x.tenant_id,x.hospital_id,x.exchange_id)
 JOIN workforce_roster_allocations a ON (a.tenant_id,a.hospital_id,a.id)=(x.tenant_id,x.hospital_id,x.allocation_id)
 WHERE x.tenant_id=? AND x.hospital_id=? AND a.roster_id=? AND e.status='pending')`, p.TenantID, hospital, roster).Scan(&pending).Error; err != nil {
		return err
	}
	if pending {
		return errDutyPending
	}
	return nil
}

func loadDutyExchangeDetail(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (dutyExchangeDetail, error) {
	d := dutyExchangeDetail{Allocations: []dutyAllocation{}}
	if err := tx.Table(dutyExchangeView).Select(dutyExchangeColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&d.Exchange).Error; err != nil {
		return d, err
	}
	ids := []uuid.UUID{d.Exchange.RequesterAllocationID}
	for _, ref := range []*uuid.UUID{d.Exchange.CounterpartAllocationID, d.Exchange.RequesterReplacementID, d.Exchange.CounterpartReplacementID, d.Exchange.RequesterRestoredID, d.Exchange.CounterpartRestoredID} {
		if ref != nil {
			ids = append(ids, *ref)
		}
	}
	return d, tx.Raw(`SELECT a.id,a.roster_id,r.name AS roster_name,r.status AS roster_status,a.staff_id,s.employee_code,s.display_name,a.roster_date::text AS roster_date,
 sr.code AS shift_code,sr.name AS shift_name,ar.code AS area_code,ar.name AS area_name,a.starts_at,a.ends_at,a.removed_version IS NULL AS active
 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
 JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_shift_revisions sr ON (sr.tenant_id,sr.hospital_id,sr.id)=(a.tenant_id,a.hospital_id,a.shift_revision_id)
 JOIN workforce_roster_config_revisions ar ON (ar.tenant_id,ar.hospital_id,ar.id)=(a.tenant_id,a.hospital_id,a.area_revision_id)
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.id IN ? ORDER BY a.roster_date,a.starts_at,a.created_at,a.id`, tenant, hospital, ids).Scan(&d.Allocations).Error
}

func (h *handlers) publishDutyExchange(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, status string, version int64) error {
	data, err := json.Marshal(workforcecontract.DutyExchangeChangedData{HospitalID: hospital.String(), ExchangeID: id.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, DutyExchangeChanged, events.Event{Type: DutyExchangeChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func respondDutyDetail(c *gin.Context, d dutyExchangeDetail) {
	if !dutyReader(c) {
		d.Exchange.Reason = ""
	}
	respond.OK(c, d)
}

// putDutyExchange files an exchange under a client-chosen id; an identical retry returns the stored exchange.
func (h *handlers) putDutyExchange(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := dutyExchangeID(c)
	if !ok {
		return
	}
	var in dutyExchangeInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	swap := in.Kind == "swap"
	if !*in.CounterpartConsent || in.Reason == "" || !validLeaveText(in.Reason) || in.RequesterAllocationID == uuid.Nil || in.CounterpartStaffID == uuid.Nil ||
		swap != (in.CounterpartAllocationID != nil) || swap && *in.CounterpartAllocationID == uuid.Nil {
		respondDutyError(c, errDutyInvalid)
		return
	}
	var result dutyExchangeDetail
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_duty", dutyLock); err != nil {
			return err
		}
		var existing dutyExchangeRow
		if err := tx.Raw(`SELECT kind,requester_allocation_id,counterpart_staff_id,counterpart_allocation_id,reason FROM workforce_duty_exchanges WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, id).Scan(&existing).Error; err != nil {
			return err
		}
		if existing.Kind != "" {
			if existing.Kind != in.Kind || existing.RequesterAllocationID != in.RequesterAllocationID || existing.CounterpartStaffID != in.CounterpartStaffID ||
				!equalPtr(existing.CounterpartAllocationID, in.CounterpartAllocationID) || existing.Reason != in.Reason {
				return errDutyReused
			}
			var err error
			result, err = loadDutyExchangeDetail(tx, p.TenantID, hospital, id)
			return err
		}
		var holder struct{ StaffID uuid.UUID }
		if err := tx.Raw(`SELECT staff_id FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, in.RequesterAllocationID).Scan(&holder).Error; err != nil {
			return err
		}
		requester := holder.StaffID
		if requester == uuid.Nil || requester == in.CounterpartStaffID {
			return errDutyReference
		}
		r := dutyExchangeRow{Kind: in.Kind, RequesterStaffID: requester, CounterpartStaffID: in.CounterpartStaffID, RequesterAllocationID: in.RequesterAllocationID, CounterpartAllocationID: in.CounterpartAllocationID}
		moves := exchangeMoves(r, false)
		if err := lockDutyRosters(tx, p, hospital, moveIDs(moves)); err != nil {
			return err
		}
		slots, err := checkDutyMoves(tx, p, hospital, moves, true)
		if err != nil {
			return err
		}
		var held bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_duty_exchange_holds WHERE tenant_id=? AND hospital_id=? AND held AND allocation_id IN ?)`, p.TenantID, hospital, moveIDs(moves)).Scan(&held).Error; err != nil {
			return err
		}
		if held {
			return errDutyHeld
		}
		dates := []string{}
		for _, s := range slots {
			dates = append(dates, s.RosterDate)
		}
		if err := tx.Exec(`INSERT INTO workforce_duty_exchanges(tenant_id,hospital_id,id,kind,requester_staff_id,counterpart_staff_id,requester_allocation_id,counterpart_allocation_id,duty_from,duty_to,counterpart_consent,reason,status,requested_by_subject,version)
 VALUES(?,?,?,?,?,?,?,?,?::date,?::date,true,?,'pending',?,1)`, p.TenantID, hospital, id, in.Kind, requester, in.CounterpartStaffID, in.RequesterAllocationID, in.CounterpartAllocationID, slices.Min(dates), slices.Max(dates), in.Reason, p.Subject).Error; err != nil {
			return err
		}
		for i, m := range moves {
			role := "requester"
			if i == 1 {
				role = "counterpart"
			}
			if err := tx.Exec(`INSERT INTO workforce_duty_exchange_holds(tenant_id,hospital_id,exchange_id,allocation_id,role,held) VALUES(?,?,?,?,?,true)`, p.TenantID, hospital, id, m.allocation, role).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_duty_exchange_events(tenant_id,hospital_id,id,exchange_id,version,status,actor_subject) VALUES(?,?,?,?,1,'pending',?)`, p.TenantID, hospital, uuid.New(), id, p.Subject).Error; err != nil {
			return err
		}
		// The reason may describe personal circumstances, so audit rows and events carry identifiers only.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.duty_exchange.filed',?,jsonb_build_object('kind',?::text,'requester_staff_id',?::uuid,'counterpart_staff_id',?::uuid,'version',1))`,
			p.TenantID, hospital, p.Subject, id, in.Kind, requester, in.CounterpartStaffID).Error; err != nil {
			return err
		}
		if err := h.publishDutyExchange(tx, p, hospital, id, "pending", 1); err != nil {
			return err
		}
		result, err = loadDutyExchangeDetail(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondDutyError(c, err)
		return
	}
	respondDutyDetail(c, result)
}

type dutyTransition struct {
	from, to string
	// separate requires the actor to differ from the filer; applies and reverses rewrite the rosters.
	separate, decides, applies, reverses bool
}

func (h *handlers) decideDutyExchange(c *gin.Context) {
	var in dutyDecisionInput
	if !decode(c, &in) {
		return
	}
	t := dutyTransition{from: "pending", to: "approved", separate: true, decides: true, applies: true}
	if in.Decision == "reject" {
		t = dutyTransition{from: "pending", to: "rejected", separate: true, decides: true}
	}
	h.transitionDutyExchange(c, t, *in.ExpectedVersion, in.Note)
}

func (h *handlers) cancelDutyExchange(c *gin.Context) {
	var in dutyCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionDutyExchange(c, dutyTransition{from: "pending", to: "cancelled"}, *in.ExpectedVersion, in.Note)
}

func (h *handlers) revokeDutyExchange(c *gin.Context) {
	var in dutyCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionDutyExchange(c, dutyTransition{from: "approved", to: "revoked", separate: true, reverses: true}, *in.ExpectedVersion, in.Note)
}

// transitionDutyExchange moves an exchange one step; replaying the step that produced the current version returns it unchanged.
func (h *handlers) transitionDutyExchange(c *gin.Context, t dutyTransition, expected int64, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := dutyExchangeID(c)
	if !ok {
		return
	}
	note = strings.TrimSpace(note)
	if !validLeaveText(note) {
		respondDutyError(c, errDutyInvalid)
		return
	}
	if (t.to == "rejected" || t.reverses) && note == "" {
		respondDutyError(c, errDutyNoteRequired)
		return
	}
	var result dutyExchangeDetail
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_duty", dutyLock); err != nil {
			return err
		}
		var r dutyExchangeRow
		if err := tx.Raw(`SELECT kind,requester_staff_id,counterpart_staff_id,requester_allocation_id,counterpart_allocation_id,requester_replacement_id,counterpart_replacement_id,status,requested_by_subject,version
 FROM workforce_duty_exchanges WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE`, p.TenantID, hospital, id).Scan(&r).Error; err != nil {
			return err
		}
		if r.Kind == "" {
			return gorm.ErrRecordNotFound
		}
		if r.Version == expected+1 && r.Status == t.to {
			var replay bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_duty_exchange_events WHERE tenant_id=? AND hospital_id=? AND exchange_id=? AND version=? AND actor_subject=? AND note=?)`, p.TenantID, hospital, id, r.Version, p.Subject, note).Scan(&replay).Error; err != nil {
				return err
			}
			if replay {
				var err error
				result, err = loadDutyExchangeDetail(tx, p.TenantID, hospital, id)
				return err
			}
		}
		if r.Version != expected {
			return errDutyStale
		}
		if r.Status != t.from {
			return errDutyState
		}
		if t.separate && r.RequestedBySubject == p.Subject {
			return errDutySelfDecision
		}
		set := "status=?,version=version+1,updated_at=clock_timestamp()"
		args := []any{t.to}
		if t.decides {
			set += ",decided_by_subject=?,decided_at=clock_timestamp()"
			args = append(args, p.Subject)
		}
		if t.applies || t.reverses {
			if err := tx.Exec(`SELECT set_config('app.workforce_rostering','authorized',true)`).Error; err != nil {
				return err
			}
			moves := exchangeMoves(r, t.reverses)
			if err := lockDutyRosters(tx, p, hospital, moveIDs(moves)); err != nil {
				return err
			}
			slots, err := checkDutyMoves(tx, p, hospital, moves, false)
			if err != nil {
				return err
			}
			moved, err := h.applyDutyMoves(tx, p, hospital, id, moves, slots)
			if err != nil {
				return err
			}
			var counterpart *uuid.UUID
			if r.Kind == "swap" {
				next := moved[moves[1].allocation]
				counterpart = &next
			}
			if t.applies {
				set += ",requester_replacement_id=?,counterpart_replacement_id=?"
			} else {
				set += ",revoked_by_subject=?,requester_restored_id=?,counterpart_restored_id=?"
				args = append(args, p.Subject)
			}
			args = append(args, moved[moves[0].allocation], counterpart)
		}
		update := tx.Exec(`UPDATE workforce_duty_exchanges SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND version=? AND status=?`, append(args, p.TenantID, hospital, id, expected, t.from)...)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errDutyStale
		}
		version := expected + 1
		if err := tx.Exec(`INSERT INTO workforce_duty_exchange_events(tenant_id,hospital_id,id,exchange_id,version,status,note,actor_subject) VALUES(?,?,?,?,?,?,?,?)`, p.TenantID, hospital, uuid.New(), id, version, t.to, note, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('previous_status',?::text,'version',?::bigint))`,
			p.TenantID, hospital, p.Subject, "workforce.duty_exchange."+t.to, id, t.from, version).Error; err != nil {
			return err
		}
		if err := h.publishDutyExchange(tx, p, hospital, id, t.to, version); err != nil {
			return err
		}
		var err error
		result, err = loadDutyExchangeDetail(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondDutyError(c, err)
		return
	}
	respondDutyDetail(c, result)
}

func (h *handlers) getDutyExchange(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := dutyExchangeID(c)
	if !ok {
		return
	}
	var result dutyExchangeDetail
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadDutyExchangeDetail(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondDutyError(c, err)
		return
	}
	respondDutyDetail(c, result)
}

// listDutyExchanges filters by status, either person and duty dates, newest filing first.
func (h *handlers) listDutyExchanges(c *gin.Context, params pagination.Params) ([]dutyExchange, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	where := "tenant_id=? AND hospital_id=?"
	args := []any{p.TenantID, hospital}
	valid := params.Limit <= 100
	if s := c.Query("status"); s != "" {
		valid = valid && slices.Contains(dutyExchangeStatuses, s)
		where += " AND status=?"
		args = append(args, s)
	}
	if s := c.Query("staff_id"); s != "" {
		staff, err := uuid.Parse(s)
		valid = valid && err == nil && staff != uuid.Nil
		where += " AND (requester_staff_id=? OR counterpart_staff_id=?)"
		args = append(args, staff, staff)
	}
	var from, to time.Time
	for _, bound := range []struct {
		name, clause string
		into         *time.Time
	}{{"from", " AND duty_to::date>=?::date", &from}, {"to", " AND duty_from::date<=?::date", &to}} {
		s := c.Query(bound.name)
		if s == "" {
			continue
		}
		day, err := time.Parse("2006-01-02", s)
		valid = valid && err == nil && day.Year() >= 2000 && day.Year() <= 2199
		*bound.into = day
		where += bound.clause
		args = append(args, s)
	}
	if !valid || !from.IsZero() && !to.IsZero() && to.Before(from) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a known status, a staff_id, from and to dates between 2000 and 2199 with from not after to, and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []dutyExchange{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(dutyExchangeView).Select(dutyExchangeColumns).Where(where, args...).Find(&rows).Error
	})
	if err != nil {
		respondDutyError(c, err)
		return nil, nil
	}
	if !dutyReader(c) {
		for i := range rows {
			rows[i].Reason = ""
		}
	}
	return rows, nil
}

func (h *handlers) dutyExchangeHistory(c *gin.Context, params pagination.Params) ([]dutyExchangeEvent, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := dutyExchangeID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []dutyExchangeEvent{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_duty_exchanges WHERE tenant_id=? AND hospital_id=? AND id=?)`, p.TenantID, hospital, id).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_duty_exchange_events").Select("id,version,status,note,actor_subject,created_at").Where("tenant_id=? AND hospital_id=? AND exchange_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondDutyError(c, err)
		return nil, nil
	}
	// Decision notes may restate the reason, so they follow the same redaction.
	if !dutyReader(c) {
		for i := range rows {
			rows[i].Note = ""
		}
	}
	return rows, nil
}

func respondDutyError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errDutySelfDecision), errors.As(err, &pg) && pg.ConstraintName == "workforce_duty_exchange_separation":
		respond.Forbidden(c, "Someone other than the person who filed the exchange must decide or revoke it.")
	case errors.As(err, &pg) && pg.ConstraintName == "workforce_duty_exchange_held":
		respond.Error(c, errDutyHeld.status, errDutyHeld.code, errDutyHeld.message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_duty_exchanges_pkey":
		respond.Error(c, errDutyReused.status, errDutyReused.code, errDutyReused.message)
	default:
		respondRosterError(c, err)
	}
}
