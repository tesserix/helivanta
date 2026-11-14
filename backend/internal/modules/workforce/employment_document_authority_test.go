package workforce

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEmploymentDocumentProofBindsSelectedResourceAndClosedPurpose(t *testing.T) {
	hospital, staff, file := uuid.New(), uuid.New(), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-file-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	proof, err := employmentDocumentProof(p, hospital, staff, file, 3, 2, documentPreparationUpload, source)
	require.NoError(t, err)
	require.Equal(t, "employment_document_preparation", proof["purpose"])
	require.Equal(t, file, proof["file_id"])
	require.Equal(t, "read", proof["stage"])
	for _, key := range []string{"review_id", "actor_staff_id", "actor_link_id", "filename", "sha256", "source_generation", "intent_hash"} {
		require.NotContains(t, proof, key)
	}
	for _, row := range []struct {
		staff, file    uuid.UUID
		version, epoch int64
		operation      documentPreparationOperation
	}{
		{uuid.Nil, file, 3, 2, documentPreparationUpload},
		{staff, uuid.Nil, 3, 2, documentPreparationUpload},
		{staff, file, 0, 2, documentPreparationUpload},
		{staff, file, 3, 0, documentPreparationUpload},
		{staff, file, 3, 2, "unknown"},
		{staff, file, 3, 2, documentPreparationList},
	} {
		_, err := employmentDocumentProof(p, hospital, row.staff, row.file, row.version, row.epoch, row.operation, source)
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	}
	listed, err := employmentDocumentProof(p, hospital, staff, uuid.Nil, 3, 2, documentPreparationList, source)
	require.NoError(t, err)
	require.NotContains(t, listed, "file_id")
}

func TestEmploymentDocumentMutationProofBindsOperationPhaseAndFile(t *testing.T) {
	hospital, staff, file := uuid.New(), uuid.New(), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-file-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: p.IdleDeadline}
	proof, err := employmentDocumentProof(p, hospital, staff, file, 3, 2, documentPreparationRegister, source)
	require.NoError(t, err)
	input := documentInput{RecordKind: "enrollment", RecordID: staff, Category: "employment_evidence", Filename: "synthetic.png", ContentType: "image/png", Size: 100, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	changed, err := employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationIntent, Input: &input}, input.SHA256)
	require.NoError(t, err)
	require.Equal(t, "intent", changed["stage"])
	require.Equal(t, "read", proof["stage"])
	require.Equal(t, input.SHA256, changed["intent_hash"])
	for _, key := range []string{"filename", "record_kind", "category", "sha256"} {
		require.NotContains(t, changed, key)
	}
	proof["operation"] = documentPreparationDownload
	_, err = employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationIntent, Input: &input}, input.SHA256)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	proof["operation"] = documentPreparationRegister
	input.Category = "identity_evidence"
	_, err = employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationIntent, Input: &input}, input.SHA256)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	proof["operation"] = documentPreparationUpload
	row := document{ID: uuid.New(), HospitalID: hospital, StaffID: staff, RecordID: staff, RecordKind: "enrollment", RecordVersion: 3, Category: "employment_evidence", Status: "upload_pending", ScanAttempt: 1}
	_, err = employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationQueue, Saved: &row, Generation: 42}, input.SHA256)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	row.ID = file
	proof["phase"] = int64(1)
	_, err = employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationQueue, Saved: &row, Generation: 42}, input.SHA256)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	proof["phase"] = int64(3)
	queued, err := employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationQueue, Saved: &row, Generation: 42}, input.SHA256)
	require.NoError(t, err)
	require.Equal(t, int64(42), queued["source_generation"])
	_, err = employmentDocumentMutationProof(proof, documentMutation{Stage: documentMutationQueue, Saved: &row, Generation: 0}, input.SHA256)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
}

func TestEmploymentDocumentBasisFailureTellsTheCallerToRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/file", func(c *gin.Context) { documentUploadError(c, errEmploymentDocumentBasis) })
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/file", nil))
	require.Equal(t, 409, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, "employment_source_changed", out["error"])
}
