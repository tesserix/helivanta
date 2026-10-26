package practitioner_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestQualificationEvidenceUsesExactQualificationRevision(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	qualification := uuid.Must(uuid.NewV7()).String()
	owner := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + qualification
	require.Equal(t, 201, testutil.Do(s.router, "PUT", owner, "steward", `{"degree":"Synthetic degree","awarding_body":"Synthetic university"}`).Code)
	path := owner + "/revisions/" + qualification + "/evidence/" + uuid.Must(uuid.NewV7()).String()
	metadata := fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(s.content), sha256.Sum256(s.content))
	got := testutil.Do(s.router, "PUT", path, "steward", metadata)
	require.Equal(t, 201, got.Code, got.Body.String())
	require.Contains(t, got.Body.String(), `"qualification_id":"`+qualification+`"`)
	require.NotContains(t, got.Body.String(), `"registration_id"`)
	request := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(s.content))
	request.Header.Set("Authorization", "Bearer steward")
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
}

func TestQualificationEvidenceScansReleasesAndReviewsWithoutRegistrationAuthority(t *testing.T) {
	s := newCredentialEvidenceScenario(t, "qualification", nil, nil)
	processor := cleanProfessionalEvidenceProcessor(t, s)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	downloaded := testutil.Do(s.router, "GET", s.path+"/content", "reader", "")
	require.Equal(t, 200, downloaded.Code, downloaded.Body.String())
	require.Equal(t, s.content, downloaded.Body.Bytes())
	metadata := testutil.Do(s.router, "GET", s.path, "reader", "")
	require.Contains(t, metadata.Body.String(), `"qualification_id":"`+s.input.QualificationID+`"`)
	require.NotContains(t, metadata.Body.String(), "registration_id")
	registrationPath := "/v1/practitioners/" + s.input.PractitionerID + "/registrations/" + s.input.QualificationID
	require.Equal(t, 201, testutil.Do(s.router, "PUT", registrationPath, "steward", `{"council_system":"synthetic.council","registration_number":"SAME-OPAQUE-ID"}`).Code)
	foreign := s.input
	foreign.RegistrationID = foreign.QualificationID
	foreign.QualificationID = ""
	_, err = processor.Process(t.Context(), foreign)
	require.Error(t, err)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.Equal(t, 404, testutil.Do(s.router, "GET", strings.Replace(s.path, "/qualifications/", "/registrations/", 1), "reader", "").Code)
	appointment := uuid.Must(uuid.NewV7()).String()
	nomination := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+appointment, "steward", nomination).Code)
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/my/steward-appointments/"+appointment+"/accept", "reviewer", `{"expected_version":1}`).Code)
	review := strings.Split(s.path, "/evidence/")[0] + "/reviews/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"2026-01-01T00:00:00Z","finding":"confirmed","evidence_ids":["%s"]}`, appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), s.input.FileID)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", review, "operator", body).Code)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", review, "registration-reviewer", body).Code)
	require.Equal(t, 400, testutil.Do(s.router, "PUT", review, "reviewer", strings.Replace(body, `"confirmed"`, `"active"`, 1)).Code)
	saved := testutil.Do(s.router, "PUT", review, "reviewer", body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	require.Contains(t, saved.Body.String(), `"finding":"confirmed"`)
	head := testutil.Do(s.router, "GET", strings.Split(s.path, "/revisions/")[0], "reviewer", "")
	require.Contains(t, head.Body.String(), `"verification_state":"unverified"`)
	selfAppointment := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+selfAppointment, "governor", fmt.Sprintf(`{"subject":"user-steward","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))).Code)
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/my/steward-appointments/"+selfAppointment+"/accept", "steward", `{"expected_version":1}`).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", review[:len(review)-36]+uuid.Must(uuid.NewV7()).String(), "steward", strings.Replace(body, appointment, selfAppointment, 1)).Code)

	correctionPath := strings.Split(s.path, "/revisions/")[0] + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	correction := testutil.Do(s.router, "PUT", correctionPath, "steward", `{"expected_version":1,"degree":"Corrected degree","awarding_body":"Synthetic university"}`)
	require.Equal(t, 201, correction.Code, correction.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", review[:len(review)-36]+uuid.Must(uuid.NewV7()).String(), "reviewer", body).Code)
	retained := testutil.Do(s.router, "PUT", review, "reviewer", body)
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.JSONEq(t, saved.Body.String(), retained.Body.String())
	revoke := testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7())))
	require.Equal(t, 201, revoke.Code, revoke.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "reader", "").Code)
	replay := testutil.Do(s.router, "PUT", review, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", review[:len(review)-36]+uuid.Must(uuid.NewV7()).String(), "reviewer", body).Code)
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var rows []struct{ Payload string }
		if err := tx.Table("outbox_events").Select("payload::text AS payload").Where("subject=?", "helivanta.in.practitioner.qualification_review_recorded.v1").Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 1)
		require.Contains(t, rows[0].Payload, `"qualification_id"`)
		require.NotContains(t, rows[0].Payload, "registration_id")
		require.NotContains(t, rows[0].Payload, "finding")
		return nil
	}))
}

func TestQualificationEvidenceManualRecoveryUsesFencedQualificationWorkflowIdentity(t *testing.T) {
	s := newCredentialEvidenceScenario(t, "qualification", nil, nil)
	failProfessionalScan(t, s)
	input := s.input
	input.RegistrationID = input.QualificationID
	input.QualificationID = ""
	oldID, err := practitioner.EvidenceWorkflowID(input)
	require.NoError(t, err)
	qualificationID, err := practitioner.EvidenceWorkflowID(s.input)
	require.NoError(t, err)
	require.NotEqual(t, oldID, qualificationID)
	path := s.path + "/scan-retries/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	receipt := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, receipt.Code, receipt.Body.String())
	status, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	next := s.input
	next.ScanAttempt = 2
	status, err = cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	replay := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, receipt.Body.String(), replay.Body.String())
}

type beforeCredentialMigration struct{ platform.Module }

func (m beforeCredentialMigration) Migrations() []tenantdb.Migration {
	return m.Module.Migrations()[:11]
}

func TestCredentialMigrationPreservesLegacyEvidenceAndRegistrationWireScope(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	router, db, collection, _ := credentialEvidenceFixture(t, "registration", files, nil, nil, beforeCredentialMigration{practitioner.New()})
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	id := uuid.Must(uuid.NewV7()).String()
	path := collection + "/" + id
	parts := strings.Split(path, "/")
	type retained struct {
		ID         uuid.UUID
		RevisionID uuid.UUID
		Size       int64
		SHA256     string
		CreatedAt  time.Time
		ExpiresAt  time.Time
	}
	var before, after retained
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-steward"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO practitioner_registration_evidence(tenant_id,practitioner_id,registration_id,revision_id,id,content_type,size,sha256,created_by) VALUES(?,?,?,?,?,'image/png',?,?,'user-steward')", testutil.TenantA, parts[3], parts[5], parts[7], id, content.Len(), fmt.Sprintf("%x", sha256.Sum256(content.Bytes()))).Error; err != nil {
			return err
		}
		return tx.Table("practitioner_registration_evidence").Select("id,revision_id,size,sha256,created_at,expires_at").Where("tenant_id=? AND id=?", testutil.TenantA, id).Take(&before).Error
	}))
	require.NoError(t, db.Migrate(t.Context(), practitioner.New().Migrations()[11:12]))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true)").Error; err != nil {
			return err
		}
		return tx.Table("practitioner_credential_evidence").Select("id,revision_id,size,sha256,created_at,expires_at").Where("tenant_id=? AND id=? AND credential_kind='registration'", testutil.TenantA, id).Take(&after).Error
	}))
	require.Equal(t, before, after)
	metadata := testutil.Do(router, "GET", path, "reader", "")
	require.Equal(t, 200, metadata.Code, metadata.Body.String())
	require.Contains(t, metadata.Body.String(), `"registration_id":"`+parts[5]+`"`)
	require.NotContains(t, metadata.Body.String(), "qualification_id")
	request := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(content.Bytes()))
	request.Header.Set("Authorization", "Bearer steward")
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := practitioner.NewEvidenceProcessor(t.Context(), professionalEvidenceWorker(t, db), files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), practitionercontract.EvidenceScanInput{TenantID: testutil.TenantA, PractitionerID: parts[3], RegistrationID: parts[5], RevisionID: parts[7], FileID: id, ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", status)
	downloaded := testutil.Do(router, "GET", path+"/content", "reader", "")
	require.Equal(t, 200, downloaded.Code, downloaded.Body.String())
	require.Equal(t, content.Bytes(), downloaded.Body.Bytes())
}
