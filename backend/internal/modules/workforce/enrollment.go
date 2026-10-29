package workforce

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

type enrollment struct {
	Staff             staff                     `json:"staff"`
	Version           int64                     `json:"version"`
	Profile           enrollmentProfile         `json:"profile"`
	Addresses         []enrollmentAddress       `json:"addresses"`
	EmergencyContacts []enrollmentContact       `json:"emergency_contacts"`
	Credentials       []enrollmentCredential    `json:"credentials"`
	Qualifications    []enrollmentQualification `json:"qualifications"`
	Experiences       []enrollmentExperience    `json:"experiences"`
}

type enrollmentInput struct {
	enrollmentRequestContextMetadata
	ExpectedVersion                 *int64                    `json:"expected_version" binding:"required,gte=0"`
	Staff                           staffInput                `json:"staff" binding:"required"`
	Profile                         enrollmentProfile         `json:"profile"`
	Addresses                       []enrollmentAddress       `json:"addresses" binding:"max=2,dive"`
	EmergencyContacts               []enrollmentContact       `json:"emergency_contacts" binding:"max=5,dive"`
	Credentials                     []enrollmentCredential    `json:"credentials" binding:"max=20,dive"`
	Qualifications                  []enrollmentQualification `json:"qualifications" binding:"max=20,dive"`
	Experiences                     []enrollmentExperience    `json:"experiences" binding:"max=20,dive"`
	providedReferences              map[string]bool
	providedQualificationReferences map[string]bool
	providedContactReferences       map[string]bool
}

func (in *enrollmentInput) UnmarshalJSON(data []byte) error {
	type plain enrollmentInput
	var value plain
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	*in = enrollmentInput(value)
	var fields struct {
		RequestContextVersion json.RawMessage              `json:"request_context_version"`
		Profile               map[string]json.RawMessage   `json:"profile"`
		Qualifications        []map[string]json.RawMessage `json:"qualifications"`
		Contacts              []map[string]json.RawMessage `json:"emergency_contacts"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields.RequestContextVersion) > 0 && in.RequestContextVersion != enrollmentRequestContextVersion {
		return errors.New("invalid enrollment request context")
	}
	in.providedReferences = map[string]bool{}
	for _, ref := range enrollmentMasterReferences(&in.Profile, enrollmentProfile{}) {
		raw, present := fields.Profile[ref.field+"_revision_id"]
		in.providedReferences[ref.field] = present
		if ref.rejectNull && present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null HR profile reference")
		}
		if raw, present := fields.Profile[ref.field]; ref.selectionOnly && present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null controlled HR field")
		}
	}
	var err error
	in.providedQualificationReferences, err = providedChildReferences(fields.Qualifications, "qualification_revision_id")
	if err != nil {
		return err
	}
	in.providedContactReferences, err = providedChildReferences(fields.Contacts, "relationship_revision_id")
	return err
}

func providedChildReferences(rows []map[string]json.RawMessage, field string) (map[string]bool, error) {
	provided := make(map[string]bool, len(rows))
	for _, row := range rows {
		var key string
		if err := json.Unmarshal(row["id"], &key); err != nil {
			return nil, err
		}
		if id, err := uuid.Parse(strings.TrimSpace(key)); err == nil {
			key = id.String()
		}
		raw, present := row[field]
		if present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, errors.New("null child master reference")
		}
		provided[key] = present
	}
	return provided, nil
}

var internationalPhone = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)
var errEnrollmentVersion = errors.New("staff changed since it was loaded")

func normalizeEnrollment(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			normalizeEnrollment(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			normalizeEnrollment(v.Index(i))
		}
	case reflect.String:
		v.SetString(strings.TrimSpace(v.String()))
	}
}

func validDate(value string) bool {
	if value == "" {
		return true
	}
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Format(time.DateOnly) == value && value >= "1900-01-01" && value <= "2200-12-31"
}

func validPeriod(start, end string) bool {
	return validDate(start) && validDate(end) && (end == "" || (start != "" && end >= start))
}

func (in *enrollmentInput) normalizeAndValidate(id uuid.UUID) bool {
	normalizeEnrollment(reflect.ValueOf(in).Elem())
	if !employeeCode.MatchString(in.Staff.EmployeeCode) || in.Staff.DisplayName == "" {
		return false
	}
	if in.Profile.EngagementType == "" {
		in.Profile.EngagementType = "employee"
	}
	if !contains(in.Profile.EngagementType, "employee", "consultant", "contractor", "locum", "trainee", "volunteer") ||
		!contains(in.Profile.Gender, "", "female", "male", "other", "prefer_not_to_say") ||
		!contains(in.Profile.BloodGroup, "", "A+", "A-", "B+", "B-", "AB+", "AB-", "O+", "O-") {
		return false
	}
	if !validDate(in.Profile.DateOfBirth) || (in.Profile.DateOfBirth != "" && in.Profile.DateOfBirth > time.Now().UTC().Format(time.DateOnly)) ||
		!validPeriod(in.Profile.StartDate, in.Profile.EndDate) || !validPeriod(in.Profile.StartDate, in.Profile.ProbationEndDate) ||
		!validPeriod(in.Profile.StartDate, in.Profile.ConfirmationDate) {
		return false
	}
	for _, phone := range []string{in.Profile.MobilePhone, in.Profile.WorkPhone} {
		if phone != "" && !internationalPhone.MatchString(phone) {
			return false
		}
	}
	if in.Profile.SupervisorID != "" {
		supervisor, err := uuid.Parse(in.Profile.SupervisorID)
		if err != nil || supervisor == uuid.Nil || supervisor == id {
			return false
		}
		in.Profile.SupervisorID = supervisor.String()
	}
	seen := map[string]bool{}
	for _, address := range in.Addresses {
		if !contains(address.Use, "current", "permanent") || address.Line1 == "" || !countryCode.MatchString(address.Country) || seen["address:"+address.Use] {
			return false
		}
		seen["address:"+address.Use] = true
	}
	for _, contact := range in.EmergencyContacts {
		key := "contact:" + strconv.Itoa(contact.Priority)
		if contact.Name == "" || contact.Relationship == "" || !internationalPhone.MatchString(contact.Phone) || seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, credential := range in.Credentials {
		key := strings.ToLower("credential:" + credential.Type + "\x00" + credential.Number + "\x00" + credential.Issuer)
		if credential.Type == "" || credential.Number == "" || credential.Issuer == "" ||
			(credential.Country != "" && !countryCode.MatchString(credential.Country)) ||
			!validDate(credential.ValidFrom) || !validDate(credential.ValidUntil) ||
			(credential.ValidFrom != "" && credential.ValidUntil != "" && credential.ValidUntil < credential.ValidFrom) || seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, qualification := range in.Qualifications {
		key := strings.ToLower("qualification:" + qualification.Qualification + "\x00" + qualification.Institution + "\x00" + strconv.Itoa(qualification.CompletionYear))
		if qualification.Qualification == "" || qualification.Institution == "" ||
			(qualification.Country != "" && !countryCode.MatchString(qualification.Country)) ||
			(qualification.CompletionYear != 0 && (qualification.CompletionYear < 1900 || qualification.CompletionYear > time.Now().UTC().Year())) || seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, experience := range in.Experiences {
		key := strings.ToLower("experience:" + experience.Employer + "\x00" + experience.Role + "\x00" + experience.StartDate)
		if experience.Employer == "" || experience.Role == "" || experience.StartDate == "" || !validPeriod(experience.StartDate, experience.EndDate) || seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, group := range []any{&in.Addresses, &in.EmergencyContacts, &in.Credentials, &in.Qualifications, &in.Experiences} {
		rows := reflect.ValueOf(group).Elem()
		if rows.IsNil() {
			rows.Set(reflect.MakeSlice(rows.Type(), 0, 0))
		}
		ids := map[string]bool{}
		for i := 0; i < rows.Len(); i++ {
			field := rows.Index(i).FieldByName("ID")
			parsed, err := uuid.Parse(field.String())
			if err != nil || parsed == uuid.Nil || ids[parsed.String()] {
				return false
			}
			field.SetString(parsed.String())
			ids[parsed.String()] = true
		}
		sort.Slice(rows.Interface(), func(i, j int) bool {
			return rows.Index(i).FieldByName("ID").String() < rows.Index(j).FieldByName("ID").String()
		})
	}
	return true
}

func contains(value string, values ...string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sameEnrollment(existing enrollment, in enrollmentInput) bool {
	return existing.Staff.EmployeeCode == in.Staff.EmployeeCode && existing.Staff.DisplayName == in.Staff.DisplayName &&
		existing.Staff.JobCategory == in.Staff.JobCategory && existing.Staff.EmploymentStatus == in.Staff.EmploymentStatus &&
		reflect.DeepEqual(existing.Profile, in.Profile) && reflect.DeepEqual(existing.Addresses, in.Addresses) &&
		reflect.DeepEqual(existing.EmergencyContacts, in.EmergencyContacts) && reflect.DeepEqual(existing.Credentials, in.Credentials) &&
		reflect.DeepEqual(existing.Qualifications, in.Qualifications) && reflect.DeepEqual(existing.Experiences, in.Experiences)
}

func enrollmentScope(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
	var grant struct{ Level string }
	if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
		return err
	}
	if grant.Level != "manager" {
		return gorm.ErrRecordNotFound
	}
	return tx.Exec(`SELECT set_config('app.workforce_enrollment', 'authorized', true)`).Error
}

func includeEnrollmentMasters(c *gin.Context, query, version string) (bool, bool) {
	versions, present := c.Request.URL.Query()[query]
	if !present {
		return false, true
	}
	if len(versions) != 1 || versions[0] != version {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This enrollment view is not supported. Refresh and retry.")
		return false, false
	}
	return true, true
}

func respondEnrollment(c *gin.Context, result enrollment, profileMasters, qualificationMasters, contactMasters, requestContext bool) {
	respond.OK(c, enrollmentResponse(result, profileMasters, qualificationMasters, contactMasters, requestContext))
}

func (h *handlers) getEnrollment(c *gin.Context) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return
	}
	projection, ok := includeEnrollmentMasters(c, enrollmentProfileMastersQuery, enrollmentProfileMastersVersion)
	if !ok {
		return
	}
	qualificationProjection, ok := includeEnrollmentMasters(c, enrollmentQualificationMastersQuery, enrollmentQualificationMastersVersion)
	if !ok {
		return
	}
	contactProjection, ok := includeEnrollmentMasters(c, enrollmentContactMastersQuery, enrollmentContactMastersVersion)
	if !ok {
		return
	}
	requestContext, ok := includeEnrollmentMasters(c, enrollmentRequestContextQuery, enrollmentRequestContextVersion)
	if !ok {
		return
	}
	if requestContext && (len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1) {
		respond.Error(c, 409, "session_changed", "Open protected Staff records in your current account and workspace.")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var result enrollment
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		var row staff
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(staffColumns+", version").Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&row).Error; err != nil {
			return err
		}
		var err error
		result, err = readEnrollment(tx, p.TenantID, hospital, id, row)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES (?, ?, ?, 'workforce.enrollment.read', ?, '{}'::jsonb)`, p.TenantID, hospital, p.Subject, id).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respondEnrollment(c, result, projection, qualificationProjection, contactProjection, requestContext)
}

func enrollmentRow(value any, tenant string, hospital, id uuid.UUID) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	row := map[string]any{}
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	for key, value := range row {
		if (strings.HasSuffix(key, "_date") || strings.HasSuffix(key, "_revision_id") || key == "date_of_birth" || key == "valid_from" || key == "valid_until" || key == "supervisor_id") && value == "" {
			row[key] = nil
		}
	}
	row["tenant_id"], row["hospital_id"], row["staff_id"] = tenant, hospital, id
	return row, nil
}

func replaceEnrollmentRows(tx *gorm.DB, tenant string, hospital, id uuid.UUID, in enrollmentInput) error {
	groups := []struct {
		table string
		value any
	}{
		{"profile", []enrollmentProfile{in.Profile}}, {"addresses", in.Addresses}, {"emergency_contacts", in.EmergencyContacts},
		{"credentials", in.Credentials}, {"qualifications", in.Qualifications}, {"experiences", in.Experiences},
	}
	for _, group := range groups {
		table := "workforce_enrollment_" + group.table
		if err := tx.Exec("DELETE FROM "+table+" WHERE tenant_id = ? AND hospital_id = ? AND staff_id = ?", tenant, hospital, id).Error; err != nil {
			return err
		}
		rows := reflect.ValueOf(group.value)
		for i := 0; i < rows.Len(); i++ {
			row, err := enrollmentRow(rows.Index(i).Interface(), tenant, hospital, id)
			if err != nil {
				return err
			}
			if err := tx.Table(table).Create(row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

var errEnrollmentFields = errors.New("invalid canonical enrollment fields")

func (h *handlers) putEnrollment(c *gin.Context) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return
	}
	projection, ok := includeEnrollmentMasters(c, enrollmentProfileMastersQuery, enrollmentProfileMastersVersion)
	if !ok {
		return
	}
	qualificationProjection, ok := includeEnrollmentMasters(c, enrollmentQualificationMastersQuery, enrollmentQualificationMastersVersion)
	if !ok {
		return
	}
	contactProjection, ok := includeEnrollmentMasters(c, enrollmentContactMastersQuery, enrollmentContactMastersVersion)
	if !ok {
		return
	}
	requestContext, ok := includeEnrollmentMasters(c, enrollmentRequestContextQuery, enrollmentRequestContextVersion)
	if !ok {
		return
	}
	if requestContext && (len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1) {
		respond.Error(c, 409, "session_changed", "Open protected Staff records in your current account and workspace.")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var in enrollmentInput
	if !decodeSized(c, &in, 32768) {
		return
	}
	if (requestContext && in.RequestContextVersion != enrollmentRequestContextVersion) || (!requestContext && in.RequestContextVersion != "") {
		respond.Error(c, 400, "invalid_request", "This protected enrollment request is not supported. Refresh and retry.")
		return
	}
	if !in.normalizeAndValidate(id) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid enrollment fields")
		return
	}
	var result enrollment
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid, ?::uuid)`, p.TenantID, hospital).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, p.TenantID+":"+hospital.String()+":"+id.String()).Error; err != nil {
			return err
		}
		var row staff
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(staffColumns+", version").Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var existing enrollment
		if err == nil {
			var loadErr error
			existing, loadErr = readEnrollment(tx, p.TenantID, hospital, id, row)
			if loadErr != nil {
				return loadErr
			}
		} else if *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		if err := validateMasterReferences(tx, p, hospital, &in.Profile, existing.Profile, in.providedReferences); err != nil {
			return err
		}
		if err := validateQualificationReferences(tx, p, hospital, in.Qualifications, existing.Qualifications, in.providedQualificationReferences); err != nil {
			return err
		}
		if err := validateContactReferences(tx, p, hospital, in.EmergencyContacts, existing.EmergencyContacts, in.providedContactReferences); err != nil {
			return err
		}
		if !in.normalizeAndValidate(id) {
			return errEnrollmentFields
		}
		if err == nil {
			if sameEnrollment(existing, in) {
				result = existing
				return nil
			}
			if *in.ExpectedVersion != row.Version {
				return errEnrollmentVersion
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_staff (tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tenant_id,hospital_id,id) DO UPDATE SET employee_code = EXCLUDED.employee_code,
		 display_name = EXCLUDED.display_name, job_category = EXCLUDED.job_category, employment_status = EXCLUDED.employment_status, updated_at = clock_timestamp()`,
			p.TenantID, hospital, id, in.Staff.EmployeeCode, in.Staff.DisplayName, in.Staff.JobCategory, in.Staff.EmploymentStatus).Error; err != nil {
			return err
		}
		if in.Staff.EmploymentStatus == "inactive" {
			if err := h.revokeInactiveStaffLinks(tx, p, hospital, id); err != nil {
				return err
			}
		}
		if err := replaceEnrollmentRows(tx, p.TenantID, hospital, id, in); err != nil {
			return err
		}
		// Surface the deferred reviewed-credential key here, as a mapped error, rather than at commit.
		if err := tx.Exec(`SET CONSTRAINTS workforce_credential_validity_enrolled IMMEDIATE`).Error; err != nil {
			return err
		}
		if err := tx.Select(staffColumns+", version").Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&row).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes)
		 VALUES (?, ?, ?, 'workforce.enrollment.changed', ?, jsonb_build_object('version', ?::bigint))`, p.TenantID, hospital, p.Subject, id, row.Version).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.StaffEnrollmentChangedData{HospitalID: hospital.String(), StaffID: id.String(), Version: row.Version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, EnrollmentChanged, events.Event{Type: EnrollmentChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		result, err = readEnrollment(tx, p.TenantID, hospital, id, row)
		return err
	})
	if errors.Is(err, errEnrollmentFields) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid enrollment fields")
		return
	}
	if errors.Is(err, errEnrollmentVersion) {
		respond.Conflict(c, "Staff changed since you opened this form. Reload before saving.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "workforce_enrollment_supervisor_acyclic" {
		respond.Error(c, http.StatusConflict, "supervisor_cycle", "Supervisor would create a circular reporting chain. Choose another supervisor or clear the link.")
		return
	}
	if errors.As(err, &pg) && pg.ConstraintName == "workforce_supervision_isolation" {
		respond.Error(c, http.StatusServiceUnavailable, "supervision_unavailable", "Staff supervision could not be verified. Refresh and retry.")
		return
	}
	if errors.As(err, &pg) && pg.Code == "55P03" {
		respond.Error(c, http.StatusServiceUnavailable, "supervision_busy", "Staff supervision is busy. Refresh and retry.")
		return
	}
	if errors.As(err, &pg) && (pg.ConstraintName == "workforce_credential_reviewed" || pg.ConstraintName == "workforce_credential_validity_enrolled") {
		respond.ConflictWithDetail(c, "A reviewed credential changes only through a credential review, and cannot be removed.", gin.H{"reason": "credential_reviewed"})
		return
	}
	if err != nil {
		if respondMasterReferenceError(c, err) {
			return
		}
		respondError(c, err)
		return
	}
	respondEnrollment(c, result, projection, qualificationProjection, contactProjection, requestContext)
}
