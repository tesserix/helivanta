package platform

import (
	"context"
	"time"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/session"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// TupleWriter is the subset of the authz client that modules may use to
// mutate authorization state. Narrow by design: modules grant and revoke,
// they never resolve — resolution belongs to the middleware.
type TupleWriter interface {
	GrantRole(ctx context.Context, tenantID, subject string, role authz.Role) error
	RevokeRole(ctx context.Context, tenantID, subject string, role authz.Role) error
	GrantPermission(ctx context.Context, tenantID string, perm authz.Permission, role authz.Role) error
	// GrantTenantRole wires a role into its tenant so that assignees of
	// the role resolve as members. Written per (tenant, role), not per
	// member: membership for any number of subjects derives through this
	// one edge.
	GrantTenantRole(ctx context.Context, tenantID string, role authz.Role) error
}

// RoleLister is the subset of the authz client that modules may use to
// discover which tenants a subject belongs to. Narrow by design,
// mirroring TupleWriter: modules read role bindings across tenants,
// they never resolve permissions within one — that belongs to the
// middleware. This is the only supported way for a module to answer
// "which tenants is this subject a member of" — RLS-forced tenant
// tables cannot answer it, because every runtime query is scoped to a
// single tenant GUC by construction.
type RoleLister interface {
	ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error)
}

// Deps is everything a module may depend on. Modules must not reach
// around it — cross-module data access goes through events (spec D6).
type Deps struct {
	DB    *tenantdb.DB
	Bus   *events.Bus
	Authz TupleWriter
	Roles RoleLister
	// SessionSigner re-mints the Helivanta session for the one operation that
	// must change a caller's identity rather than read it: switching
	// hospitals (#838, spec D3). Post-cutover this is the ONLY way a
	// module can issue a credential — there is no more external identity
	// provider to mint against, and no more Firebase client to wrap. Set
	// by main.go from the SAME key/kid/issuer login mints with (cmd/api's
	// sessionSigner); nil in tests that do not exercise it, in which case
	// the switch route fails closed rather than issuing nothing and
	// claiming success — mirroring the old Tokens field's nil behaviour.
	SessionSigner *session.Signer
	// SessionTTL is the lifetime stamped on a re-minted session cookie.
	// Must be the same value cfg.SessionTTL feeds iam.NewLoginHandlers
	// (cmd/api/main.go) — two different TTLs for the same signer would
	// mean a tenant switch silently outlives or undercuts the bound login
	// established (spec D4).
	SessionTTL time.Duration
	// SessionSecureCookie mirrors the `secure` flag iam.NewLoginHandlers
	// sets on the session cookie at login (true outside dev). A re-mint
	// that got this wrong would either downgrade an HTTPS-only cookie to
	// plaintext-eligible or, in dev, refuse to set a cookie the browser
	// will not send back over http://.
	SessionSecureCookie bool
	// Reconcile ensures a tenant's permission tuples match the registry.
	// Set by main.go; nil in tests that do not exercise it.
	Reconcile func(ctx context.Context, tenantID string) error
	// IdleTimeout is cfg.IdleTimeout (#848 Task 4): the window POST
	// /v1/auth/session/activity re-opens on every call it serves. The
	// SAME value iam.LoginDeps.IdleTimeout feeds a genuine login with —
	// there is exactly one idle window in this system, and the two
	// endpoints that can ever grant a fresh one must agree on its
	// length.
	//
	// This route's rate budget does NOT travel through Deps: unlike
	// login (mounted outside the authenticated chain, so it needs its
	// own ratelimit.Limiter/Rule pair threaded in by hand), the activity
	// route is registered through platform.Router and so already passes
	// through ratelimit.Middleware — its budget is bootstrap.RateLimitConfig's
	// Tight map (keyed "POST /v1/auth/session/activity"), the same
	// mechanism and the same Limiter instance every other authenticated
	// route already uses, with no extra wiring here.
	IdleTimeout time.Duration
}

// Module is the registration contract from issue #2.
type Module interface {
	Name() string
	Migrations() []tenantdb.Migration
	// Permissions declares every permission this module's routes use and
	// which system roles hold it. authz.RoleTenantAdmin is implicit — the
	// reconciler grants it everything, so never list it here.
	Permissions() []authz.Grant
	Routes(r *Router, deps Deps)
	// Publishes declares every subject this module publishes.
	//
	// Declarative like Permissions and Migrations, and for the same
	// reason: it makes a property checkable that is otherwise scattered
	// across call sites. Two CI checks build on it — that every consumer
	// subscribes to something a module actually publishes, and that no
	// two modules publish the same subject — and the event catalogue
	// (#667) is its union, so the catalogue cannot drift from the code.
	//
	// Entries must be constants from this module's own contract package,
	// not bare strings; TestPublishesUsesContractConstants enforces it.
	// A module that publishes nothing returns nil.
	Publishes() []string
	// DirectedSubjects declares which of this module's Publishes() may
	// carry a DestinationTenantID — that is, may create a record in a
	// tenant other than the one that published (design D4, #932).
	//
	// Must be a subset of Publishes(); Registry.Register fails at boot
	// otherwise, and archtest checks the same property over the real
	// modules. Entries must be constants from this module's own contract
	// package, like Publishes().
	//
	// This list is deliberately the smallest reviewable surface in the
	// codebase: everything on it can cross an organisational boundary.
	// A module with no cross-tenant events returns nil.
	DirectedSubjects() []string
	Consumers(deps Deps) []events.Consumer
	// Broadcasts declares this module's fanout subscriptions — every
	// replica hears every message, unlike Consumers, where replicas
	// sharing a durable name compete and exactly one wins (#781). Most
	// modules return nil; iam uses it to invalidate its revocation cache
	// the instant another replica revokes a credential.
	Broadcasts(deps Deps) []events.Broadcast
}
