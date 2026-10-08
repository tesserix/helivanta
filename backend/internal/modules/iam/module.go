// Package iam owns tenant membership and roles. Postgres is the system
// of record; OpenFGA is the decision point, and is fully rebuildable
// from these tables by the reconciler.
package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermMemberManage authz.Permission = "iam.member.manage"

// PermCredentialRevoke gates POST /subjects/:subject/revoke. It lists no
// system roles, so tenant_admin is the only role that holds it — the
// reconciler's implicit grant to tenant_admin, and nothing else. Revoking
// a person's credentials platform-wide (see revocationHandlers.adminRevoke
// in signout.go) is not an authority any clinical role should carry.
const PermCredentialRevoke authz.Permission = "iam.credential.revoke" //nolint:gosec // permission name, not a credential value

// Module owns tenant membership and the credential-revocation watermark.
//
// checker is the same *RevocationChecker instance the authentication
// middleware (pkg/authn.Middleware) consults on every request. It MUST be
// constructed once by the caller (cmd/api/main.go, via
// internal/bootstrap) and threaded into both New and the middleware — two
// separately constructed checkers would compile and pass most tests while
// the module's sign-out/revoke handlers silently invalidate a cache
// nothing on the request path ever reads (#781).
type Module struct {
	checker *RevocationChecker
	// userState re-checks a subject's Zitadel state on renewal (#916,
	// design spec D3). See SetUserStateChecker.
	userState UserStateChecker
}

func New(checker *RevocationChecker) *Module { return &Module{checker: checker} }

// SetUserStateChecker wires the Zitadel login-client state re-check
// POST /v1/auth/renew needs (#916, design spec D3). It is a separate
// setter, not a New parameter, so the ~20 existing call sites that build
// a Module via New(nil) purely to exercise a different route (me_test.go,
// routes_test.go, activity_test.go, internal/archtest's allModules(), …)
// keep compiling unchanged — mirroring how checker itself is nil at
// every one of those call sites already. Only cmd/api/main.go calls this
// in production. Left unset, renew fails closed (503) rather than
// silently admitting — see renewalHandlers.renew's own comment on the
// nil-userState branch.
func (m *Module) SetUserStateChecker(u UserStateChecker) *Module {
	m.userState = u
	return m
}

func (m *Module) Name() string { return "iam" }

// CheckerForTest exposes the module's revocation checker for tests that
// need to assert it is the SAME instance the caller constructed and
// handed to authn.Middleware — see TestNewRegistryGivesIAMTheExactCheckerPassedIn
// in internal/bootstrap.
func (m *Module) CheckerForTest() *RevocationChecker { return m.checker }

// UserStateCheckerForTest exposes the module's Zitadel state checker for
// the same reason CheckerForTest exists (Review Round 1, IMPORTANT 2):
// bootstrap.Modules threads userState into SetUserStateChecker, and
// nothing previously asserted that wiring actually landed. Two
// separately-constructed *loginclient.Client instances would compile and
// pass every other test while POST /v1/auth/renew quietly used the
// wrong one — or, dropping the .SetUserStateChecker(userState) call
// entirely, every renewal would 503 for every clinician (#916's own
// symptom, from a different cause) with go test ./... still green. See
// TestNewRegistryGivesIAMTheExactUserStateCheckerPassedIn in
// internal/bootstrap, which mirrors
// TestNewRegistryGivesIAMTheExactCheckerPassedIn exactly.
func (m *Module) UserStateCheckerForTest() UserStateChecker { return m.userState }

// Permissions declares iam.member.manage and iam.credential.revoke.
// Neither lists a role: tenant_admin is implicit, and revoking a
// person's credentials platform-wide is not an authority any clinical
// role should carry.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermMemberManage},
		{Permission: PermCredentialRevoke},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_iam",
		SQL: `
			CREATE TABLE iam_roles (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  key text NOT NULL,
			  label text NOT NULL,
			  is_system boolean NOT NULL DEFAULT false,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, key)
			);
			ALTER TABLE iam_roles ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_roles FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_roles
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			CREATE TABLE iam_members (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  subject text NOT NULL,
			  role_key text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, subject, role_key)
			);
			ALTER TABLE iam_members ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_members
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON iam_members (tenant_id, subject);
			CREATE INDEX ON iam_members (subject);`,
	}, {
		ID: "0002_iam",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON iam_roles
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			ALTER POLICY tenant_isolation ON iam_members
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}, {
		ID: "0003_iam",
		// Not tenant-scoped, and therefore an explicit LintRLS allowlist
		// entry rather than a table that quietly has no tenant_id: a GIP
		// subject is global, so a revocation is global. It holds no
		// tenant data and no PHI.
		//
		// The watermark only ever moves forward (see the GREATEST upsert
		// in revocation.go): a retried or late write must never be able
		// to resurrect a revoked credential.
		SQL: `
			CREATE TABLE iam_credential_revocations (
			  subject    text PRIMARY KEY,
			  revoked_at timestamptz NOT NULL,
			  reason     text NOT NULL CHECK (reason IN ('sign_out','admin_revoke')),
			  actor      text NOT NULL,
			  updated_at timestamptz NOT NULL DEFAULT now()
			);`,
	}, {
		// login_attempt holds the Zitadel session between the password step and
		// the factor step, because spec D2 forbids handing a Zitadel session
		// token to the browser.
		//
		// DELIBERATELY NOT TENANT-SCOPED, and deliberately WITHOUT the forced-RLS
		// boilerplate every tenant table in this repo carries: login happens
		// BEFORE a tenant is selected, so there is no tenant_id to scope on and
		// inventing one would mean fabricating a value the user has not chosen
		// yet. A reviewer applying the RLS convention here by reflex produces a
		// policy that matches nothing.
		ID: "0004_iam",
		SQL: `
CREATE TABLE IF NOT EXISTS login_attempt (
  auth_request_id       text PRIMARY KEY,
  zitadel_session_id    text        NOT NULL,
  zitadel_session_token text        NOT NULL,
  subject               text        NOT NULL,
  factor_attempts       int         NOT NULL DEFAULT 0,
  expires_at            timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS login_attempt_expires_at_idx ON login_attempt (expires_at);
`,
	}, {
		// #894. platform.Reconcile reads iam_members across every tenant to
		// turn memberships into OpenFGA tuples, and it is the only reader
		// that does — SELECT is all it needs, so SELECT is all it gets.
		//
		// The system role's table privileges live in migrations, per table, beside
		// the table they concern — not in the harness and not in a values file.
		//
		// BYPASSRLS decides whether the row POLICIES apply; it confers no table
		// privileges whatsoever. helivanta_system therefore authenticates fine and
		// then fails every statement with "permission denied for table", which
		// reads nothing like the missing GRANT that caused it. Confirmed against
		// production the moment the role was created: rolbypassrls was true and
		// has_table_privilege(...,'iam_members','SELECT') was false.
		//
		// Grants are explicit per table rather than ALL TABLES or ALTER DEFAULT
		// PRIVILEGES, deliberately. This role already bypasses tenant isolation
		// completely; the table list is the only thing left bounding what it can
		// reach, so a new tenant table should NOT become readable by it merely by
		// existing. A future whole-system reader adds its own grant here, and that
		// line is the reviewable moment.
		ID: "0005_iam",
		SQL: `
			GRANT SELECT ON iam_members TO helivanta_system;
`,
	}, {
		// #948, spec D4. A login_attempt row now records whether the password
		// step left it ENROLLING (forceMfa, nothing enrolled: the user was
		// handed a fresh TOTP secret and must confirm it via
		// POST /v1/auth/login/enroll) or awaiting an already-enrolled factor
		// (POST /v1/auth/login/factor). Each route refuses the other's row
		// before any Zitadel call, so a caller probing which state an attempt
		// is in learns nothing. The default is the fail-closed one: a row
		// without the flag can only take the factor path, which cannot
		// succeed without a verified TOTP.
		//
		// Same table, same deliberate absence of tenant scoping and RLS as
		// 0004_iam — see that migration's comment.
		ID: "0006_iam",
		SQL: `
ALTER TABLE login_attempt ADD COLUMN IF NOT EXISTS enrolling boolean NOT NULL DEFAULT false;
`,
	}}
}

type role struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
}

func (role) TableName() string { return "iam_roles" }

type member struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Subject   string    `json:"subject"`
	RoleKey   string    `json:"role_key"`
	CreatedAt time.Time `json:"created_at"`
}

func (member) TableName() string { return "iam_members" }

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (m member) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

type grantRequest struct {
	Subject string `json:"subject" binding:"required,max=200"`
	RoleKey string `json:"role_key" binding:"required,max=100"`
}

// knownRole reports whether key is a system role. Custom roles are
// supported by the model but not yet creatable, so anything else is a
// client error rather than a silently-dead grant. Delegates to
// authz.KnownRole, the single source of truth for the system role list,
// so this HTTP-path check and Reconcile's raw-SQL-path check
// (internal/platform/reconcile.go) can never drift apart.
func knownRole(key string) bool {
	return authz.KnownRole(authz.Role(key))
}

type memberHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// grant adds a role to a subject, or repairs a stuck FGA tuple by
// re-publishing member_granted for a grant that already exists at the
// row level.
func (h *memberHandlers) grant(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var req grantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if !knownRole(req.RoleKey) {
		respond.BadRequest(c, fmt.Errorf("unknown role %q", req.RoleKey))
		return
	}
	row := member{TenantID: tenantUUID, Subject: req.Subject, RoleKey: req.RoleKey}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		// Re-granting an existing role is a no-op at the row level,
		// not a conflict: the caller's intent is already satisfied.
		// An explicit ON CONFLICT DO NOTHING (rather than GORM's
		// FirstOrCreate, which has sharp edges around which fields
		// populate the created row) both sets tenant_id on the
		// inserted row and makes "already granted" unambiguous:
		// row.ID stays uuid.Nil because Postgres returns no row for
		// a skipped insert.
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "subject"}, {Name: "role_key"}},
			DoNothing: true,
		}).Create(&row).Error; err != nil {
			return err
		}
		if row.ID == uuid.Nil {
			// Already granted at the row level, but the outbox
			// event still publishes unconditionally below (fetch
			// the existing row first so the response carries its
			// real id). Re-granting is a request-time repair
			// mechanism for FGA drift, not the only one:
			// platform.Reconcile (internal/platform/reconcile.go)
			// re-applies every iam_members row's role tuple at
			// boot, so a lost grant event also self-heals the next
			// time any replica boots. This re-grant path stays
			// useful because it repairs immediately, on request,
			// without waiting for a boot. Tuple writes are
			// idempotent, so the redundant write on a normal
			// re-grant is harmless.
			if err := tx.Where("subject = ? AND role_key = ?", req.Subject, req.RoleKey).First(&row).Error; err != nil {
				return err
			}
		}
		data, err := json.Marshal(iamcontract.MemberChangedData(req))
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectMemberGranted, events.Event{
			Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not grant role")
		return
	}
	respond.Accepted(c, gin.H{"id": row.ID.String()})
}

// revoke removes a subject's role, publishing member_revoked
// unconditionally so a re-issued revoke can still clear a stuck FGA
// tuple even when no row matched.
func (h *memberHandlers) revoke(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	subject, roleKey := c.Param("subject"), c.Param("role")
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Where("subject = ? AND role_key = ?", subject, roleKey).
			Delete(&member{}).Error; err != nil {
			return err
		}
		// Publishes unconditionally, even if no row matched, for the
		// same repair-mechanism reason as the grant path above: a
		// re-issued revoke must still be able to clear a stuck FGA
		// tuple.
		data, err := json.Marshal(iamcontract.MemberChangedData{Subject: subject, RoleKey: roleKey})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectMemberRevoked, events.Event{
			Type: "MemberRevoked", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not revoke role")
		return
	}
	respond.Accepted(c, gin.H{"subject": subject, "role_key": roleKey})
}

// list returns one page of this tenant's members, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint. This is the endpoint the pagination contract matters most
// for: it governs who has access to a hospital, so the 500-row hard cap
// it previously carried was an access-control blind spot, not a UX
// annoyance (#816).
func (h *memberHandlers) list(c *gin.Context, p pagination.Params) ([]member, error) {
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []member
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	})
	return rows, err
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/iam")
	members := &memberHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/members", PermMemberManage, members.grant)
	g.DELETE("/members/:subject/:role", PermMemberManage, members.revoke)
	platform.ListRoute(g, "/members", PermMemberManage, members.list)
	g.GET("/roles", PermMemberManage, listRoles)

	m.registerMe(g, deps)

	rev := &revocationHandlers{db: deps.DB, bus: deps.Bus, checker: m.checker, roles: deps.Roles}
	// NoTenantMembership: a member whose membership was just revoked must
	// still be able to end their own session (#781). adminRevoke is the
	// opposite — it requires both PermCredentialRevoke (membership
	// examined as usual) and, inside the handler, that the target is a
	// member of the ACTING admin's own tenant.
	g.POST("/me/sign-out", authz.NoTenantMembership, rev.signOut)
	g.POST("/subjects/:subject/revoke", PermCredentialRevoke, rev.adminRevoke)

	m.registerActivity(r, deps)
	m.registerRenewal(r, deps)
}

// registerActivity mounts POST /v1/auth/session/activity (#848 Task 4,
// spec D4) at /auth, a sibling of /iam under the SAME r ("/v1") this
// module was handed — not nested under g ("/v1/iam") — because the
// route belongs beside the other pre-session auth endpoints
// (/v1/auth/login) by URL shape, even though, unlike login, it runs
// INSIDE the authenticated chain: it re-mints an EXISTING session
// rather than creating one, so it needs a live principal
// authn.Middleware has already verified, and is declared through
// platform.Router like any other route rather than mounted on the raw
// engine the way login is (see bootstrap.MountUnauthenticated and
// login.go's own doc comment on why login cannot be here).
//
// NoTenantMembership, the same self-service convention registerMe's
// routes use just above (see its doc comment): this endpoint's only
// job is proving a human is still at the keyboard and extending the
// window that fact earns, which has nothing to do with what the caller
// is a member of. A caller whose tenant membership was revoked mid-shift
// must still be able to call it — the alternative would 404 an activity
// signal from someone who is legitimately still present, for a reason
// unrelated to whether they are present.
func (m *Module) registerActivity(r *platform.Router, deps platform.Deps) {
	act := newActivityHandlers(deps.SessionSigner, deps.SessionTTL, deps.SessionSecureCookie, deps.IdleTimeout)
	auth := r.Group("/auth")
	auth.POST("/session/activity", authz.NoTenantMembership, act.activity)
}

// registerRenewal mounts POST /v1/auth/renew (#916, design spec D1/D3)
// beside /v1/auth/session/activity — same reasoning, same placement:
// inside the authenticated /v1 chain, under /auth rather than nested
// under g ("/v1/iam"), so authn.Middleware's cookie verification, the
// #781 revocation check and the idle-deadline gate ALL run before this
// handler, satisfying the design brief's first refusal condition
// ("absent, invalid, expired, or past idle_deadline") with no
// verification code duplicated here.
//
// authz.NoTenantMembership, not authz.Public: both decline to declare a
// permission — authz.Middleware's OpenFGA Resolve call still runs on
// this route either way, regardless of which one is chosen, so the
// choice here is NOT about avoiding a second call to OpenFGA. What
// NoTenantMembership additionally skips is authz.RequireMembership —
// platform.Router's own membership GATE, backed by
// authz.MembershipChecker.IsMember (pkg/authz/tenant.go), a DIFFERENT
// mechanism from the one this endpoint is required to reuse
// (platform.RoleLister.ListRoles + hasBindingForTenant — the exact
// check Login performs, see renewalHandlers.renew). Marking this route
// Public would run RequireMembership FIRST, giving the request a
// second, differently-shaped membership CHECK ahead of the one the
// design brief asks for — disagreeing with it on status code (403 vs.
// this handler's 404) and therefore on what a mismatch discloses.
// NoTenantMembership keeps renewalHandlers.renew the ONLY membership
// check this route makes, the same reason sign-out and the activity
// endpoint both take it: a route that must run its own bespoke
// membership logic should not also pay for a second, generic gate ahead
// of it.
func (m *Module) registerRenewal(r *platform.Router, deps platform.Deps) {
	h := newRenewalHandlers(deps.SessionSigner, deps.Roles, m.userState,
		deps.SessionTTL, deps.SessionSecureCookie)
	auth := r.Group("/auth")
	auth.POST("/renew", authz.NoTenantMembership, h.renew)
}

// Publishes declares the three events iam emits: member_granted and
// member_revoked drive the FGA sync consumers, credential_revoked is a
// broadcast every replica hears (see Broadcasts below).
func (m *Module) Publishes() []string {
	return []string{
		iamcontract.SubjectMemberGranted,
		iamcontract.SubjectMemberRevoked,
		iamcontract.SubjectCredentialRevoked,
	}
}

// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: this module writes no table on
// behalf of another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

// Broadcasts invalidates this replica's revocation cache the instant
// another replica revokes a credential (#781). The durable truth is
// Postgres — see RevocationChecker.RevokedAfter's read-through and its
// 5-minute TTL backstop — so a dropped or duplicated broadcast degrades
// to that TTL, never to incorrectness.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast {
	return []events.Broadcast{{
		Subject: iamcontract.SubjectCredentialRevoked,
		Handle: func(ctx context.Context, evt events.Event) {
			var data iamcontract.CredentialRevokedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				slog.ErrorContext(ctx, "credential_revoked: undecodable payload", "err", err)
				return
			}
			m.checker.Invalidate(data.Subject)
		},
	}}
}
