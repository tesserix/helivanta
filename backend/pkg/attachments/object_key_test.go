package attachments_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func TestObjectKeyPartitionsOpaqueOwnershipByServerUploadDateUTC(t *testing.T) {
	scope := attachments.ObjectScope{
		TenantID:   uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		HospitalID: uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		OwnerKind:  "patient",
		OwnerID:    uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		RecordID:   uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		FileID:     uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		Version:    1,
		UploadedAt: time.Date(2026, 10, 15, 0, 5, 0, 0, time.FixedZone("hospital", 11*60*60)),
	}
	key, err := attachments.ObjectKey(scope)
	require.NoError(t, err)
	require.Equal(t, "t/11111111-1111-4111-8111-111111111111/h/22222222-2222-4222-8222-222222222222/o/patient/33333333-3333-4333-8333-333333333333/r/44444444-4444-4444-8444-444444444444/d/2026/10/14/f/55555555-5555-4555-8555-555555555555/v/1", key)
	scope.OwnerKind = "staff"
	staffKey, err := attachments.ObjectKey(scope)
	require.NoError(t, err)
	require.NotEqual(t, key, staffKey)
}

func TestObjectKeyRejectsMissingScopeAndPathInjection(t *testing.T) {
	scope := attachments.ObjectScope{TenantID: uuid.New(), HospitalID: uuid.New(), OwnerKind: "staff", OwnerID: uuid.New(), RecordID: uuid.New(), FileID: uuid.New(), Version: 1, UploadedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
	for _, tc := range []struct {
		name   string
		change func(*attachments.ObjectScope)
	}{
		{"tenant", func(s *attachments.ObjectScope) { s.TenantID = uuid.Nil }},
		{"hospital", func(s *attachments.ObjectScope) { s.HospitalID = uuid.Nil }},
		{"owner", func(s *attachments.ObjectScope) { s.OwnerID = uuid.Nil }},
		{"record", func(s *attachments.ObjectScope) { s.RecordID = uuid.Nil }},
		{"file", func(s *attachments.ObjectScope) { s.FileID = uuid.Nil }},
		{"path traversal", func(s *attachments.ObjectScope) { s.OwnerKind = "staff/../../patient" }},
		{"zero version", func(s *attachments.ObjectScope) { s.Version = 0 }},
		{"negative version", func(s *attachments.ObjectScope) { s.Version = -1 }},
		{"missing date", func(s *attachments.ObjectScope) { s.UploadedAt = time.Time{} }},
		{"UTC date outside supported range", func(s *attachments.ObjectScope) {
			s.UploadedAt = time.Date(1970, 1, 1, 0, 0, 0, 0, time.FixedZone("hospital", 3600))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := scope
			tc.change(&invalid)
			key, err := attachments.ObjectKey(invalid)
			require.Error(t, err)
			require.Empty(t, key)
		})
	}
}

func TestOrganisationObjectKeyNeedsNoHospitalAndCannotAliasClinicalOwnership(t *testing.T) {
	scope := attachments.ObjectScope{
		TenantID: uuid.Must(uuid.NewV7()), OwnerKind: "organisation", OwnerID: uuid.Must(uuid.NewV7()),
		RecordID: uuid.Must(uuid.NewV7()), FileID: uuid.Must(uuid.NewV7()), Version: 2,
		UploadedAt: time.Date(2026, 10, 16, 0, 5, 0, 0, time.FixedZone("organisation", 5*60*60)),
	}
	key, err := attachments.ObjectKey(scope)
	require.NoError(t, err)
	require.Equal(t, "t/"+scope.TenantID.String()+"/o/organisation/"+scope.OwnerID.String()+"/r/"+scope.RecordID.String()+"/d/2026/10/15/f/"+scope.FileID.String()+"/v/2", key)
	for _, field := range []string{"tenant", "owner", "record", "file"} {
		t.Run(field, func(t *testing.T) {
			changed := scope
			switch field {
			case "tenant":
				changed.TenantID = uuid.Must(uuid.NewV7())
			case "owner":
				changed.OwnerID = uuid.Must(uuid.NewV7())
			case "record":
				changed.RecordID = uuid.Must(uuid.NewV7())
			case "file":
				changed.FileID = uuid.Must(uuid.NewV7())
			}
			other, e := attachments.ObjectKey(changed)
			require.NoError(t, e)
			require.NotEqual(t, key, other)
		})
	}
	scope.HospitalID = uuid.Must(uuid.NewV7())
	_, err = attachments.ObjectKey(scope)
	require.Error(t, err, "organisation keys must reject ambiguous hospital ownership")
	for _, kind := range []string{"patient", "staff"} {
		scope.HospitalID = uuid.Nil
		scope.OwnerKind = kind
		_, err = attachments.ObjectKey(scope)
		require.Error(t, err)
	}
}
