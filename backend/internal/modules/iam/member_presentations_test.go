package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
)

type memberPresentationIdentity func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error)

func (f memberPresentationIdentity) AssignmentCandidateIdentity(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
	return f(ctx, tenant, subject)
}

func presentationHarness(t *testing.T, identity memberPresentationIdentity) (*gin.Engine, *tenantdb.DB, context.Context) {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "staff": testutil.TenantA, "foreign-admin": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}, "staff": {iam.PermMemberManage}, "foreign-admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil).SetAssignmentIdentityReader(identity).SetMembershipIdentityReader(membershipIdentity{})},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin'),(?,'user-staff','staff_member'),(?,'candidate','staff_member')`, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-foreign-admin','tenant_admin')`, testutil.TenantB).Error
	}))
	return r, db, ctx
}

func TestMemberPresentationsReadNamesAndRolesWithoutChangingAccess(t *testing.T) {
	identity := memberPresentationIdentity(func(_ context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		if tenant != testutil.TenantA {
			return nil, errors.New("wrong tenant identity lookup")
		}
		return &customercontract.AssignmentCandidateIdentity{DisplayName: "Asha Rao", Email: "asha@example.test"}, nil
	})
	r, db, ctx := presentationHarness(t, identity)
	res := membershipCommandRequest(r, http.MethodPost, "/v1/iam/member-presentations", "admin", `{"subjects":["candidate","user-admin"]}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var out iam.MemberPresentations
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	require.Equal(t, testutil.TenantA, out.TenantID.String())
	require.Equal(t, "user-admin", out.ActorSubject)
	require.Len(t, out.Data, 2)
	require.Equal(t, "candidate", out.Data[0].Subject)
	require.Equal(t, "Asha Rao", *out.Data[0].Name)
	require.True(t, out.Data[0].Available)
	require.Equal(t, "staff_member", out.Data[0].Roles[0].Key)
	require.Equal(t, "Staff member (membership only)", out.Data[0].Roles[0].Label)
	require.Equal(t, "Tenant Admin", out.Data[1].Roles[0].Label)
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("audit_events").Where("action='iam.member_presentation.read'").Count(&count).Error)
		require.EqualValues(t, 2, count)
		require.NoError(t, tx.Table("iam_members").Count(&count).Error)
		require.EqualValues(t, 3, count)
		return nil
	}))
}

func TestMemberPresentationsRefuseInvalidOrUnauthorizedDisclosureBeforeIdentityIO(t *testing.T) {
	var calls atomic.Int32
	r, _, _ := presentationHarness(t, func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		calls.Add(1)
		return nil, errors.New("should not read identity")
	})
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{"staff", `{"subjects":["candidate"]}`, 403},
		{"foreign-admin", `{"subjects":["candidate"]}`, 404},
		{"admin", `{"subjects":["missing"]}`, 404},
		{"admin", `{"subjects":["candidate","candidate"]}`, 400},
		{"admin", `{"subjects":[]}`, 400},
		{"admin", `{"subjects":["candidate"],"tenant_id":"other"}`, 400},
		{"admin", `{"subjects":["candidate"],"subjects":["user-admin"]}`, 400},
	} {
		res := membershipCommandRequest(r, http.MethodPost, "/v1/iam/member-presentations", tc.token, tc.body)
		require.Equal(t, tc.status, res.Code, res.Body.String())
	}
	require.Zero(t, calls.Load())
}

func TestMemberPresentationsRejectChangedSessionAndOversizedBatches(t *testing.T) {
	var calls atomic.Int32
	r, _, _ := presentationHarness(t, func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		calls.Add(1)
		return nil, nil
	})
	for _, header := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/iam/member-presentations", strings.NewReader(`{"subjects":["candidate"]}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer admin")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		value := "different-account"
		if header == "X-Helivanta-Expected-Tenant" {
			value = testutil.TenantB
		}
		request.Header.Set(header, value)
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		require.Equal(t, 409, response.Code, response.Body.String())
	}
	subjects := []string{}
	for i := range 21 {
		subjects = append(subjects, "account-"+strconv.Itoa(i))
	}
	body, err := json.Marshal(map[string]any{"subjects": subjects})
	require.NoError(t, err)
	response := membershipCommandRequest(r, http.MethodPost, "/v1/iam/member-presentations", "admin", string(body))
	require.Equal(t, 400, response.Code, response.Body.String())
	require.Zero(t, calls.Load())
}

func TestMemberPresentationsWithholdIdentityOnFailureAndMembershipReplacement(t *testing.T) {
	for _, mode := range []string{"unavailable", "replaced", "inactive", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			var db *tenantdb.DB
			var ctx context.Context
			var router *gin.Engine
			var candidate iam.MembershipCandidate
			identity := memberPresentationIdentity(func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
				if mode == "unavailable" {
					return nil, errors.New("PRIVATE provider details")
				}
				if mode == "inactive" {
					return nil, nil
				}
				if mode == "revoked" {
					err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
						return iam.NewRevocationChecker(db).RevokeTx(tx, "user-admin", time.Now(), "admin_revoke", "user-admin")
					})
					if err != nil {
						t.Errorf("revoke test credential: %v", err)
						return nil, err
					}
				} else {
					apply := func(input iam.MembershipCommandInput) (iam.MembershipCommand, error) {
						var receipt iam.MembershipCommand
						body, err := json.Marshal(input)
						if err != nil {
							return receipt, err
						}
						response := membershipCommandRequest(router, http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), "admin", string(body))
						if response.Code != http.StatusAccepted {
							t.Errorf("concurrent membership command: %d %s", response.Code, response.Body.String())
							return receipt, errors.New("membership command refused")
						}
						err = json.Unmarshal(response.Body.Bytes(), &receipt)
						return receipt, err
					}
					removed, err := apply(iam.MembershipCommandInput{Operation: "remove", BasisID: candidate.ID, Subject: candidate.Subject, RoleKey: candidate.RoleKey, ExpectedMembershipEpoch: candidate.MembershipEpoch, Reason: "Synthetic concurrent membership removal"})
					if err != nil {
						return nil, err
					}
					_, err = apply(iam.MembershipCommandInput{Operation: "recover", BasisID: removed.ID, Subject: candidate.Subject, RoleKey: "staff_member", ExpectedMembershipEpoch: removed.ResultMembershipEpoch, Reason: "Synthetic concurrent ordinary membership recovery"})
					if err != nil {
						return nil, err
					}
				}

				return &customercontract.AssignmentCandidateIdentity{DisplayName: "PRIVATE NAME", Email: "private@example.test"}, nil
			})
			r, database, harnessContext := presentationHarness(t, identity)
			db, ctx = database, harnessContext
			router = r
			if mode == "replaced" {
				response := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-candidates?limit=50", "admin", "")
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var members struct{ Data []iam.MembershipCandidate }
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &members))
				for _, member := range members.Data {
					if member.Subject == "candidate" {
						candidate = member
					}
				}
				require.NotEqual(t, uuid.Nil, candidate.ID)
			}
			res := membershipCommandRequest(r, http.MethodPost, "/v1/iam/member-presentations", "admin", `{"subjects":["candidate"]}`)
			status := map[string]int{"unavailable": 503, "replaced": 409, "inactive": 200, "revoked": 401}[mode]
			require.Equal(t, status, res.Code, res.Body.String())
			require.NotContains(t, res.Body.String(), "PRIVATE")
			require.NotContains(t, res.Body.String(), "private@example.test")
			if mode == "inactive" {
				require.Contains(t, res.Body.String(), `"available":false`)
				require.NotContains(t, res.Body.String(), `"name"`)
			}
		})
	}
}
