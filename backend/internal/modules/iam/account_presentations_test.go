package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // Own module HTTP contract.
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
)

func TestAccountPresentationsReadOwnedHumansWithoutCreatingMemberships(t *testing.T) {
	router, db, ctx := presentationHarness(t, func(_ context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		require.Equal(t, testutil.TenantA, tenant)
		if subject == "foreign-account" {
			return nil, nil
		}
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Verified " + subject, Email: subject + "@example.test"}, nil
	})
	response := membershipCommandRequest(router, http.MethodPost, "/v1/iam/account-presentations", "admin", `{"subjects":["candidate","new-human","foreign-account"]}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result iam.AccountPresentations
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, testutil.TenantA, result.TenantID.String())
	require.Equal(t, "user-admin", result.ActorSubject)
	require.Len(t, result.Data, 2)
	require.Equal(t, "Verified candidate", *result.Data[0].Name)
	require.Equal(t, "Staff member (membership only)", result.Data[0].Roles[0].Label)
	require.Equal(t, "new-human", result.Data[1].Subject)
	require.True(t, result.Data[1].Available)
	require.Empty(t, result.Data[1].Roles)
	require.NotContains(t, response.Body.String(), "foreign-account")
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Count(&count).Error)
		require.EqualValues(t, 3, count)
		resource := uuid.NewSHA1(uuid.MustParse(testutil.TenantA), []byte("new-human"))
		require.NoError(t, tx.Table("audit_events").Where("resource_type='iam_accounts' AND resource_id=? AND action IN ?", resource, []string{"iam.account_presentation.requested", "iam.account_presentation.read"}).Count(&count).Error)
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestAccountPresentationsConcealUnavailableOrForeignAccounts(t *testing.T) {
	for _, token := range []string{"admin", "foreign-admin"} {
		t.Run(token, func(t *testing.T) {
			router, _, _ := presentationHarness(t, func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
				return nil, nil
			})
			response := membershipCommandRequest(router, http.MethodPost, "/v1/iam/account-presentations", token, `{"subjects":["outside-account"]}`)
			require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "outside-account")
		})
	}
}

func TestAccountPresentationsRefuseUnauthorizedAndMalformedReadsBeforeIdentityIO(t *testing.T) {
	var calls atomic.Int32
	router, _, _ := presentationHarness(t, func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		calls.Add(1)
		return nil, errors.New("must not query identity")
	})
	for _, test := range []struct {
		token, body string
		status      int
	}{
		{"staff", `{"subjects":["candidate"]}`, 403},
		{"admin", `{"subjects":[]}`, 400},
		{"admin", `{"subjects":["candidate","candidate"]}`, 400},
		{"admin", `{"subjects":["candidate"],"tenant_id":"foreign"}`, 400},
		{"admin", `{"subjects":["candidate"],"subjects":["user-admin"]}`, 400},
		{"admin", `{"subjects":["` + strings.Repeat("x", 201) + `"]}`, 400},
	} {
		response := membershipCommandRequest(router, http.MethodPost, "/v1/iam/account-presentations", test.token, test.body)
		require.Equal(t, test.status, response.Code, response.Body.String())
	}
	require.Zero(t, calls.Load())
}
