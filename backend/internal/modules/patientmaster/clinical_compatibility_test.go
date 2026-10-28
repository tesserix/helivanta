package patientmaster

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestRetainedSelfV1DeclarationPreservesSignedBytes(t *testing.T) {
	// Exported from the pre-proxy serializer at registry PR #1337 head 3bb59ed.
	payload, err := os.ReadFile("testdata/clinical_self_v1.json")
	require.NoError(t, err)
	id := uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	row := clinicalConsent{ID: id, HospitalID: id, PatientID: id, Subject: "synthetic-owner", AuthorityID: id, ReviewRequestID: id, NoticeID: id, RecipientID: id, Purpose: "care_coordination", Fingerprint: "synthetic-fingerprint", Body: string(payload), CapturedAt: at, ValidUntil: at.Add(time.Hour)}
	body, err := decodeClinicalConsent(row, authn.Principal{TenantID: id.String(), Subject: "synthetic-owner"})
	require.NoError(t, err)
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, string(payload), string(encoded), "changing retained signing bytes invalidates old HMAC receipts")
	require.Empty(t, clinicalClaimRelationship(body.AuthorityKind), "self requests must omit the new private field during rolling upgrades")
	require.Equal(t, "child", clinicalClaimRelationship("parental"))
	require.Equal(t, "ward", clinicalClaimRelationship("appointed_guardian"))
	row.IsProxy = true
	_, err = decodeClinicalConsent(row, authn.Principal{TenantID: id.String(), Subject: "synthetic-owner"})
	require.ErrorIs(t, err, errClinicalIntegrity, "a self v1 declaration cannot be relabelled as proxy authority")
}
