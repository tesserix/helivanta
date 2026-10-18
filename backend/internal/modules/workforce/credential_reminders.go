package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const CredentialReminderWorkflowName = "workforce-credential-reminders-v1"
const CredentialReminderActivityName = "workforce-credential-reminder-advance-v1"
const CredentialReminderTaskQueue = "workforce-credential-reminders-v1"
const credentialReminderActor = "service:workforce-credential-reminders"
const credentialReminderCycles = 20
const credentialReminderColumns = "r.id,r.staff_id,s.employee_code,s.display_name,r.credential_id,v.type,r.valid_until::text AS valid_until,r.validity_version,r.threshold_days,r.due_on::text AS due_on,r.status,r.acknowledged_by_subject,r.acknowledged_at,r.created_at" //nolint:gosec // G101: a column list, not a secret

var (
	errReminderPolicyInvalid  = errors.New("reminder thresholds are invalid")
	errReminderPolicyVersion  = errors.New("reminder policy version conflict")
	errReminderSuperseded     = errors.New("reminder superseded")
	errReminderWorkerIdentity = errors.New("credential reminder database identity is not authorized")
)

// The inbox pages by due date then reminder id, which is unique per hospital.
func (r credentialReminder) PageKey() (time.Time, uuid.UUID) {
	day, _ := time.Parse(time.DateOnly, r.DueOn)
	return day, r.ID
}

// reminderPlan is what one cycle owes: the threshold to write now, if any, and when the next one falls due.
type reminderPlan struct {
	write *int
	next  *time.Time
}

// planReminder reads thresholds in descending order; a threshold is due once its date is on or before local today.
func planReminder(thresholds []int, validUntil time.Time, today time.Time) reminderPlan {
	var plan reminderPlan
	for _, t := range thresholds {
		due := validUntil.AddDate(0, 0, -t)
		if due.After(today) {
			if plan.next == nil {
				plan.next = &due
			}
			continue
		}
		plan.write = &t
	}
	return plan
}

// reminderWake is the next threshold's local midnight, capped at 24 hours so policy and zone changes land within a day.
func reminderWake(next *time.Time, loc *time.Location, now time.Time) time.Time {
	wake := now.Add(24 * time.Hour)
	if next != nil {
		if midnight := time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc); midnight.Before(wake) {
			return midnight
		}
	}
	return wake
}

func parseIntArray(text string) ([]int, error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(text, "{"), "}")
	if inner == "" {
		return []int{}, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

func intArrayLiteral(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func loadReminderPolicy(tx *gorm.DB, tenant string, hospital uuid.UUID) (credentialReminderPolicy, error) {
	var row struct {
		Version          int64
		Thresholds       string
		CreatedBySubject string
		CreatedAt        time.Time
	}
	res := tx.Raw(`SELECT version,thresholds::text AS thresholds,created_by_subject,created_at FROM workforce_credential_reminder_policies WHERE tenant_id=? AND hospital_id=? ORDER BY version DESC LIMIT 1`, tenant, hospital).Scan(&row)
	if res.Error != nil {
		return credentialReminderPolicy{}, res.Error
	}
	if res.RowsAffected == 0 {
		return credentialReminderPolicy{Thresholds: []int{}}, nil
	}
	thresholds, err := parseIntArray(row.Thresholds)
	if err != nil {
		return credentialReminderPolicy{}, err
	}
	return credentialReminderPolicy{Version: row.Version, Thresholds: thresholds, CreatedBySubject: &row.CreatedBySubject, CreatedAt: &row.CreatedAt}, nil
}

func validReminderInput(in workforcecontract.CredentialReminderInput) bool {
	if in.Version < 1 {
		return false
	}
	for _, value := range []string{in.TenantID, in.HospitalID, in.StaffID, in.CredentialID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	return true
}

func reminderChanged(tenant, hospital, staff, credential, reminder, status string, threshold int, version int64) (events.Event, error) {
	data, err := json.Marshal(workforcecontract.CredentialReminderChangedData{HospitalID: hospital, StaffID: staff, CredentialID: credential, ReminderID: reminder, Status: status, ThresholdDays: threshold, Version: version})
	if err != nil {
		return events.Event{}, err
	}
	return events.Event{Type: CredentialReminderChanged, Version: 1, TenantID: tenant, Data: data}, nil
}

// supersedeReminders closes open reminders matching where and returns their change events.
func supersedeReminders(tx *gorm.DB, tenant, hospital, staff, credential string, where string, args ...any) ([]events.Event, error) {
	var closed []struct {
		ID              uuid.UUID
		ThresholdDays   int
		ValidityVersion int64
	}
	query := `UPDATE workforce_credential_reminders SET status='superseded',superseded_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=? AND status='open' AND ` + where + ` RETURNING id,threshold_days,validity_version`
	if err := tx.Raw(query, append([]any{tenant, hospital, staff, credential}, args...)...).Scan(&closed).Error; err != nil {
		return nil, err
	}
	out := make([]events.Event, 0, len(closed))
	for _, r := range closed {
		evt, err := reminderChanged(tenant, hospital, staff, credential, r.ID.String(), "superseded", r.ThresholdDays, r.ValidityVersion)
		if err != nil {
			return nil, err
		}
		out = append(out, evt)
	}
	return out, nil
}

// CredentialReminderAdvancer writes due reminders as the least-privilege document worker role.
type CredentialReminderAdvancer struct {
	db *tenantdb.DB
}

func NewCredentialReminderAdvancer(ctx context.Context, db *tenantdb.DB) (*CredentialReminderAdvancer, error) {
	if db == nil {
		return nil, errReminderWorkerIdentity
	}
	var authorized bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_document_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole AND NOT rolcreatedb FROM pg_roles WHERE rolname=current_user`).Scan(&authorized).Error
	})
	if err != nil || !authorized {
		return nil, errReminderWorkerIdentity
	}
	return &CredentialReminderAdvancer{db: db}, nil
}

// Advance fails closed: a moved revision ends the schedule, and a missing zone or policy writes nothing.
func (a *CredentialReminderAdvancer) Advance(ctx context.Context, in workforcecontract.CredentialReminderInput) (workforcecontract.CredentialReminderStep, error) {
	if !validReminderInput(in) {
		return workforcecontract.CredentialReminderStep{}, temporal.NewNonRetryableApplicationError("credential reminder input invalid", "reminder_invalid", nil)
	}
	var step workforcecontract.CredentialReminderStep
	err := a.db.WithTenant(tenantdb.WithActor(ctx, credentialReminderActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_reminder_staff',?,true),set_config('app.workforce_reminder_credential',?,true)`, in.HospitalID, in.StaffID, in.CredentialID).Error; err != nil {
			return err
		}
		var current struct {
			Version    int64
			ValidUntil string
		}
		res := tx.Raw(`SELECT version,valid_until::text AS valid_until FROM workforce_credential_validity WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=?`, in.TenantID, in.HospitalID, in.StaffID, in.CredentialID).Scan(&current)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 || current.Version != in.Version {
			step.Done = true
			return nil
		}
		validUntil, err := time.Parse(time.DateOnly, current.ValidUntil)
		if err != nil {
			return err
		}
		hospital, err := uuid.Parse(in.HospitalID)
		if err != nil {
			return err
		}
		loc, now, err := credentialClock(tx, in.TenantID, hospital)
		if err != nil {
			return err
		}
		step.WakeAt = now.Add(24 * time.Hour)
		if loc == nil {
			// Every zone's today is before UTC tomorrow, so a lapsed credential cannot fall due later.
			step.Done = !validUntil.After(now.UTC().AddDate(0, 0, -2))
			return nil
		}
		today, _ := time.Parse(time.DateOnly, now.In(loc).Format(time.DateOnly))
		policy, err := loadReminderPolicy(tx, in.TenantID, hospital)
		if err != nil {
			return err
		}
		if policy.Version == 0 {
			step.Done = validUntil.Before(today)
			return nil
		}
		plan := planReminder(policy.Thresholds, validUntil, today)
		if plan.write != nil {
			if err := a.write(tx, in, policy.Version, *plan.write, validUntil); err != nil {
				return err
			}
		}
		smallest := policy.Thresholds[len(policy.Thresholds)-1]
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_credential_reminders WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=? AND validity_version=? AND threshold_days=?)`,
			in.TenantID, in.HospitalID, in.StaffID, in.CredentialID, in.Version, smallest).Scan(&step.Done).Error; err != nil {
			return err
		}
		step.WakeAt = reminderWake(plan.next, loc, now)
		return nil
	})
	if err == nil {
		return step, nil
	}
	// Raw SQL errors must never enter durable history.
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "workforce_credential_reminder_current" {
		return workforcecontract.CredentialReminderStep{}, temporal.NewApplicationError("credential reminder revision moved", "reminder_stale")
	}
	return workforcecontract.CredentialReminderStep{}, temporal.NewApplicationError("credential reminder unavailable", "reminder_unavailable")
}

func (a *CredentialReminderAdvancer) write(tx *gorm.DB, in workforcecontract.CredentialReminderInput, policyVersion int64, threshold int, validUntil time.Time) error {
	var row struct{ ID uuid.UUID }
	res := tx.Raw(`INSERT INTO workforce_credential_reminders(tenant_id,hospital_id,staff_id,credential_id,validity_version,policy_version,threshold_days,channel,valid_until,due_on,status)
 VALUES(?,?,?,?,?,?,?,'hr_inbox',?::date,?::date,'open') ON CONFLICT ON CONSTRAINT workforce_credential_reminder_once DO NOTHING RETURNING id`,
		in.TenantID, in.HospitalID, in.StaffID, in.CredentialID, in.Version, policyVersion, threshold, validUntil.Format(time.DateOnly), validUntil.AddDate(0, 0, -threshold).Format(time.DateOnly)).Scan(&row)
	if res.Error != nil || res.RowsAffected == 0 {
		return res.Error
	}
	opened, err := reminderChanged(in.TenantID, in.HospitalID, in.StaffID, in.CredentialID, row.ID.String(), "open", threshold, in.Version)
	if err != nil {
		return err
	}
	closed, err := supersedeReminders(tx, in.TenantID, in.HospitalID, in.StaffID, in.CredentialID, "validity_version=? AND threshold_days>?", in.Version, threshold)
	if err != nil {
		return err
	}
	for _, evt := range append(closed, opened) {
		if err := events.Enqueue(tx, CredentialReminderChanged, evt); err != nil {
			return err
		}
	}
	return nil
}

// CredentialReminderWorkflow only sleeps and asks; the 24-hour cap lets policy and zone changes land within a day.
func CredentialReminderWorkflow(ctx workflow.Context, in workforcecontract.CredentialReminderInput) error {
	if !validReminderInput(in) {
		return temporal.NewNonRetryableApplicationError("credential reminder input invalid", "reminder_invalid", nil)
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    10,
		},
	})
	for range credentialReminderCycles {
		var step workforcecontract.CredentialReminderStep
		if err := workflow.ExecuteActivity(ctx, CredentialReminderActivityName, in).Get(ctx, &step); err != nil {
			return err
		}
		if step.Done {
			return nil
		}
		// A wake time already past still waits a minute, so a clock disagreement cannot spin.
		if err := workflow.Sleep(ctx, max(step.WakeAt.Sub(workflow.Now(ctx)), time.Minute)); err != nil {
			return err
		}
	}
	return workflow.NewContinueAsNewError(ctx, CredentialReminderWorkflowName, in)
}

// CredentialReminderWorkflowID names one schedule per reviewed revision.
func CredentialReminderWorkflowID(in workforcecontract.CredentialReminderInput) (string, error) {
	if !validReminderInput(in) {
		return "", errCredentialInvalid
	}
	return fmt.Sprintf("wf-cred-rem-%s-%s-%s-%s-v%d", in.TenantID, in.HospitalID, in.StaffID, in.CredentialID, in.Version), nil
}

func reminderEventInput(evt events.Event) (workforcecontract.CredentialReminderInput, error) {
	if evt.Type != CredentialValidityChanged || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return workforcecontract.CredentialReminderInput{}, errors.New("credential validity envelope invalid")
	}
	var data workforcecontract.CredentialValidityChangedData
	dec := json.NewDecoder(strings.NewReader(string(evt.Data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil || dec.More() {
		return workforcecontract.CredentialReminderInput{}, errors.New("credential validity payload invalid")
	}
	in := workforcecontract.CredentialReminderInput{TenantID: evt.TenantID, HospitalID: data.HospitalID, StaffID: data.StaffID, CredentialID: data.CredentialID, Version: data.Version}
	if !validReminderInput(in) {
		return workforcecontract.CredentialReminderInput{}, errors.New("credential validity scope invalid")
	}
	return in, nil
}

// CredentialReminderConsumer acknowledges only an accepted, matching schedule.
func CredentialReminderConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "workforce-credential-reminders", Subject: CredentialValidityChanged,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := reminderEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, err := reminderEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("credential reminder workflow client unavailable")
			}
			id, err := CredentialReminderWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: CredentialReminderTaskQueue,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, CredentialReminderWorkflowName, in)
			if err == nil {
				return nil
			}
			var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
			if !errors.As(err, &duplicate) || duplicate.RunId == "" {
				return errors.New("credential reminder workflow start unavailable")
			}
			it := c.GetWorkflowHistory(ctx, id, duplicate.RunId, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
			if !it.HasNext() {
				return errors.New("credential reminder workflow receipt unavailable")
			}
			first, err := it.Next()
			if err != nil {
				return errors.New("credential reminder workflow receipt unavailable")
			}
			started := first.GetWorkflowExecutionStartedEventAttributes()
			if started == nil || started.GetWorkflowType().GetName() != CredentialReminderWorkflowName || started.GetTaskQueue().GetName() != CredentialReminderTaskQueue {
				return errors.New("credential reminder workflow receipt mismatch")
			}
			var previous workforcecontract.CredentialReminderInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("credential reminder workflow receipt mismatch")
			}
			return nil
		},
	}
}

func (h *handlers) getReminderPolicy(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result credentialReminderPolicy
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		var err error
		result, err = loadReminderPolicy(tx, p.TenantID, hospital)
		return err
	})
	if err != nil {
		respondReminderError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) putReminderPolicy(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in credentialReminderPolicyInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	thresholds := slices.Clone(in.Thresholds)
	slices.SortFunc(thresholds, func(a, b int) int { return b - a })
	if len(slices.Compact(slices.Clone(thresholds))) != len(thresholds) {
		respondReminderError(c, errReminderPolicyInvalid)
		return
	}
	var result credentialReminderPolicy
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_enrollment", "credential-reminder-policy"); err != nil {
			return err
		}
		current, err := loadReminderPolicy(tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		if current.Version > 0 && slices.Equal(current.Thresholds, thresholds) {
			result = current
			return nil
		}
		if current.Version != *in.ExpectedVersion {
			return errReminderPolicyVersion
		}
		version := current.Version + 1
		if err := tx.Exec(`INSERT INTO workforce_credential_reminder_policies(tenant_id,hospital_id,version,thresholds,created_by_subject) VALUES(?,?,?,?::integer[],?)`,
			p.TenantID, hospital, version, intArrayLiteral(thresholds), p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.credential_reminder_policy.changed',?,jsonb_build_object('version',?::bigint,'thresholds',?::jsonb))`,
			p.TenantID, hospital, p.Subject, hospital, version, "["+strings.Trim(intArrayLiteral(thresholds), "{}")+"]").Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.CredentialReminderPolicyChangedData{HospitalID: hospital.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, CredentialReminderPolicyChanged, events.Event{Type: CredentialReminderPolicyChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		result, err = loadReminderPolicy(tx, p.TenantID, hospital)
		return err
	})
	if err != nil {
		respondReminderError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listCredentialReminders(c *gin.Context, params pagination.Params) ([]credentialReminder, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	status := c.DefaultQuery("status", "open")
	if !slices.Contains([]string{"open", "acknowledged", "superseded"}, status) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use status open, acknowledged or superseded.")
		return nil, nil
	}
	var rows []credentialReminder
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		q := reminderQuery(tx, p.TenantID, hospital).Where("r.status=?", status).Order("r.due_on, r.id").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(r.due_on,r.id) > (?::date,?)", params.After.CreatedAt.UTC().Format(time.DateOnly), params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.credential_reminders.read',?,jsonb_build_object('status',?::text))`,
			p.TenantID, hospital, p.Subject, hospital, status).Error
	})
	if err != nil {
		respondReminderError(c, err)
		return nil, nil
	}
	return rows, nil
}

func reminderQuery(tx *gorm.DB, tenant string, hospital uuid.UUID) *gorm.DB {
	return tx.Table("workforce_credential_reminders r").
		Joins("JOIN workforce_staff s ON s.tenant_id=r.tenant_id AND s.hospital_id=r.hospital_id AND s.id=r.staff_id").
		Joins("JOIN workforce_credential_validity v ON v.tenant_id=r.tenant_id AND v.hospital_id=r.hospital_id AND v.staff_id=r.staff_id AND v.credential_id=r.credential_id").
		Select(credentialReminderColumns).Where("r.tenant_id=? AND r.hospital_id=?", tenant, hospital)
}

func (h *handlers) acknowledgeCredentialReminder(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("reminder"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "reminder")
		return
	}
	var result credentialReminder
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_enrollment", "credential-reminder:"+id.String()); err != nil {
			return err
		}
		var saved struct {
			Status          string
			StaffID         uuid.UUID
			CredentialID    uuid.UUID
			ThresholdDays   int
			ValidityVersion int64
		}
		if err := tx.Table("workforce_credential_reminders").Select("status,staff_id,credential_id,threshold_days,validity_version").
			Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&saved).Error; err != nil {
			return err
		}
		switch saved.Status {
		case "superseded":
			return errReminderSuperseded
		case "open":
			if err := tx.Exec(`UPDATE workforce_credential_reminders SET status='acknowledged',acknowledged_by_subject=?,acknowledged_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND status='open'`,
				p.Subject, p.TenantID, hospital, id).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.credential_reminder.acknowledged',?,jsonb_build_object('reminder_id',?::uuid,'credential_id',?::uuid))`,
				p.TenantID, hospital, p.Subject, saved.StaffID, id, saved.CredentialID).Error; err != nil {
				return err
			}
			evt, err := reminderChanged(p.TenantID, hospital.String(), saved.StaffID.String(), saved.CredentialID.String(), id.String(), "acknowledged", saved.ThresholdDays, saved.ValidityVersion)
			if err != nil {
				return err
			}
			if err := h.bus.Publish(tx, CredentialReminderChanged, evt); err != nil {
				return err
			}
		}
		return reminderQuery(tx, p.TenantID, hospital).Where("r.id=?", id).Take(&result).Error
	})
	if err != nil {
		respondReminderError(c, err)
		return
	}
	respond.OK(c, result)
}

// supersedeOlderReminders runs inside approval so an older revision's reminders close with the revision itself.
func supersedeOlderReminders(h *handlers, tx *gorm.DB, tenant string, hospital, staff, credential uuid.UUID, version int64) error {
	closed, err := supersedeReminders(tx, tenant, hospital.String(), staff.String(), credential.String(), "validity_version<?", version)
	if err != nil {
		return err
	}
	for _, evt := range closed {
		if err := h.bus.Publish(tx, CredentialReminderChanged, evt); err != nil {
			return err
		}
	}
	return nil
}

func respondReminderError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errReminderPolicyInvalid), isPG && pg.ConstraintName == "workforce_credential_reminder_thresholds":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Give 1 to 6 different reminder days from 0 to 365.")
	case errors.Is(err, errReminderPolicyVersion), isPG && pg.ConstraintName == "workforce_credential_reminder_policy_version":
		respond.ConflictWithDetail(c, "The reminder policy changed since you opened it. Reload before saving.", gin.H{"reason": "version_conflict"})
	case errors.Is(err, errReminderSuperseded), isPG && pg.ConstraintName == "workforce_credential_reminder_closed":
		respond.ConflictWithDetail(c, "A newer reminder or review replaced this reminder.", gin.H{"reason": "reminder_superseded"})
	default:
		respondError(c, err)
	}
}
