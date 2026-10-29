package workforce

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func directoryContext(c *gin.Context) (bool, bool) {
	versions, present := c.Request.URL.Query()[staffDirectoryContextQuery]
	if !present {
		return false, true
	}
	if len(versions) != 1 || versions[0] != staffDirectoryContextVersion {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This Staff directory view is not supported. Refresh and retry.")
		return false, false
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respond.Error(c, http.StatusConflict, "session_changed", "Open Staff in your current account and workspace.")
		return false, false
	}
	return true, true
}

func respondDirectoryStaff(c *gin.Context, row staff, projection bool) {
	if !projection {
		respond.OK(c, row)
		return
	}
	respond.OK(c, struct {
		staff
		staffDirectoryReadMetadata
	}{row, staffDirectoryReadMetadata{Version: row.Version, DirectoryContextVersion: staffDirectoryContextVersion}})
}

var errStaffDirectoryVersion = errors.New("staff directory version changed")

type staffDirectoryInput struct {
	staffInput
	staffDirectoryWriteMetadata
	metadataSupplied bool
}

func (in *staffDirectoryInput) UnmarshalJSON(data []byte) error {
	type wire staffDirectoryInput
	var value wire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, expected := fields["expected_version"]
	_, marker := fields["directory_context_version"]
	*in = staffDirectoryInput(value)
	in.metadataSupplied = expected || marker
	return nil
}

func lockDirectoryManager(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
	var grant struct{ Level string }
	if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
		return err
	}
	if grant.Level != "manager" {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func checkDirectoryVersion(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, in staffDirectoryInput) error {
	var row staff
	read := func() error {
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(staffColumns+", version").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).First(&row).Error
	}
	err := read()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if *in.ExpectedVersion != 0 {
			return errStaffDirectoryVersion
		}
		if err := tx.Exec(`INSERT INTO workforce_staff (tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES (?,?,?,?,?,?,?) ON CONFLICT (tenant_id,hospital_id,id) DO NOTHING`, p.TenantID, hospital, id, in.EmployeeCode, in.DisplayName, in.JobCategory, in.EmploymentStatus).Error; err != nil {
			return err
		}
		err = read()
	}
	if err != nil {
		return err
	}
	identical := row.EmployeeCode == in.EmployeeCode && row.DisplayName == in.DisplayName && row.JobCategory == in.JobCategory && row.EmploymentStatus == in.EmploymentStatus
	if row.Version != *in.ExpectedVersion && !identical {
		return errStaffDirectoryVersion
	}
	return nil
}
