package journey

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestIAMGrantMembership(t *testing.T) {
	for _, failure := range []string{"none", "tenant_edge", "member_edge"} {
		t.Run(failure, func(t *testing.T) {
			ctx := t.Context()
			upstream, err := url.Parse(testinfra.StartOpenFGA(t))
			require.NoError(t, err)
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			var blocked atomic.Bool
			var failures atomic.Int32
			blocked.Store(failure != "none")
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { blocked.Store(false); close(release) })
			t.Cleanup(unblock)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/write") {
					body, readErr := io.ReadAll(r.Body)
					if readErr != nil {
						http.Error(w, "read test request", http.StatusBadRequest)
						return
					}
					r.Body.Close()
					r.Body = io.NopCloser(bytes.NewReader(body))
					var payload struct {
						Writes struct {
							Tuples []struct {
								User     string `json:"user"`
								Relation string `json:"relation"`
								Object   string `json:"object"`
							} `json:"tuple_keys"`
						} `json:"writes"`
					}
					if err := json.Unmarshal(body, &payload); err != nil {
						http.Error(w, "decode test request", http.StatusBadRequest)
						return
					}
					for _, tuple := range payload.Writes.Tuples {
						tenantEdge := tuple.Relation == "granted_role" && tuple.Object == authz.TenantObject(testutil.TenantA) && tuple.User == authz.RoleObject(testutil.TenantA, authz.RoleSupplyCompliance)
						memberEdge := tuple.Relation == "assignee" && tuple.Object == authz.RoleObject(testutil.TenantA, authz.RoleSupplyCompliance)
						if blocked.Load() && ((failure == "tenant_edge" && tenantEdge) || (failure == "member_edge" && memberEdge)) {
							if failures.Add(1) == 1 {
								// A non-retriable SDK response forces the NATS consumer to retry.
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusBadRequest)
								_, _ = w.Write([]byte(`{"code":"validation_error","message":"synthetic OpenFGA write failure"}`))
								return
							}
							select {
							case <-release:
							case <-r.Context().Done():
								return
							}
						}
					}
				}
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			fga, err := authz.NewClient(ctx, server.URL, testinfra.IsolationKey(t))
			require.NoError(t, err)
			require.NoError(t, fga.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
			require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "user-admin", authz.RoleTenantAdmin))
			modules := []platform.Module{iam.New(nil), supply.New()}
			registry := platform.NewRegistry()
			for _, module := range modules {
				require.NoError(t, registry.Register(module))
			}
			require.NoError(t, platform.ReconcileTenant(ctx, registry, fga, testutil.TenantA))
			r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
				Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB},
				Perms: map[string][]authz.Permission{
					"admin": {iam.PermMemberManage}, "reviewer": {supply.PermDirectoryRead},
					"peer": {supply.PermDirectoryRead}, "foreign": {supply.PermDirectoryRead},
				},
				Writer: fga, Membership: fga, Roles: fga,
				Modules: modules,
			})
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error
			}))
			directory := "/v1/supply/parties"
			require.Equal(t, http.StatusForbidden, do(r, "GET", directory, "reviewer", "").Code)
			grant := func(subject string) {
				t.Helper()
				response := do(r, "POST", "/v1/iam/members", "admin", `{"subject":"`+subject+`","role_key":"supply_compliance"}`)
				require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
			}
			grant("user-reviewer")
			if failure != "none" {
				require.Eventually(t, func() bool { return failures.Load() >= 2 }, 10*time.Second, 10*time.Millisecond,
					"a failed external write must reach NATS redelivery")
				require.Equal(t, http.StatusForbidden, do(r, "GET", directory, "reviewer", "").Code,
					"an incomplete external grant must remain denied")
				var processed int64
				require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
					return tx.Table("processed_events").Where("consumer = ?", "iam-fga-sync").Count(&processed).Error
				}))
				require.EqualValues(t, 1, processed, "the claim commits before external projection; redelivery must still retry AfterCommit")
				unblock()
			}
			waitMember := func(subject, token string, want bool) {
				t.Helper()
				require.Eventually(t, func() bool {
					member, checkErr := fga.IsMember(ctx, subject, testutil.TenantA)
					return checkErr == nil && member == want
				}, 10*time.Second, 20*time.Millisecond, "role grant must establish tenant membership without restart")
				response := do(r, "GET", directory, token, "")
				if want {
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				} else {
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				}
			}
			waitMember("user-reviewer", "reviewer", true)
			permissions, err := fga.Resolve(ctx, "user-reviewer", testutil.TenantA)
			require.NoError(t, err)
			require.True(t, permissions.Has(supply.PermLicenseReview))
			require.False(t, permissions.Has(iam.PermMemberManage), "membership must not grant administrator privileges")
			grant("user-reviewer")
			grant("user-peer")
			waitMember("user-peer", "peer", true)
			var members int64
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("iam_members").Where("role_key = ?", authz.RoleSupplyCompliance).Count(&members).Error
			}))
			require.EqualValues(t, 2, members, "repeat grant must not duplicate membership rows")
			var target struct {
				ID    uuid.UUID
				Epoch int64
			}
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT m.id,e.epoch FROM iam_members m JOIN iam_subject_membership_epochs e USING(tenant_id,subject) WHERE m.subject='user-reviewer' AND m.role_key='supply_compliance'`).Scan(&target).Error
			}))
			require.NotEqual(t, uuid.Nil, target.ID)
			input, err := json.Marshal(iam.MembershipCommandInput{Operation: "remove", BasisID: target.ID, Subject: "user-reviewer", RoleKey: "supply_compliance", ExpectedMembershipEpoch: target.Epoch, Reason: "Remove synthetic reviewer membership after journey"})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(string(input)))
			req.Header.Set("Authorization", "Bearer admin")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
			waitMember("user-reviewer", "reviewer", false)
			waitMember("user-peer", "peer", true)
			member, err := fga.IsMember(ctx, "user-peer", testutil.TenantB)
			require.NoError(t, err)
			require.False(t, member, "a role in tenant A cannot establish membership in tenant B")
			require.Equal(t, http.StatusForbidden, do(r, "GET", directory, "foreign", "").Code)
			require.Eventually(t, func() bool {
				var processed, published int64
				err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Table("processed_events").Where("consumer IN ?", []string{"iam-fga-sync", "iam-fga-sync-revoke"}).Count(&processed).Error; err != nil {
						return err
					}
					return tx.Table("outbox_events").Where("published_at IS NOT NULL").Count(&published).Error
				})
				return err == nil && processed == 4 && published == 4
			}, 10*time.Second, 20*time.Millisecond, "three grants and one revoke each commit exactly one receipt and published outbox row")
		})
	}
}
