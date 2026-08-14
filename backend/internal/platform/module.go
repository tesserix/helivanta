package platform

import (
	"context"

	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
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
	// Tokens mints a custom token carrying a tenant_id claim, for the one
	// operation that must change a caller's identity rather than read it:
	// switching hospitals. Unlike Authz/Roles this is not a narrowed
	// mirror of a bigger client — authn.TokenMinter is already exactly one
	// method, and deliberately does not expose the Firebase auth client
	// it wraps. Set by main.go; nil in tests that do not exercise it, in
	// which case the switch route fails closed rather than issuing
	// nothing and claiming success.
	Tokens authn.TokenMinter
	// TokenRevoker revokes a subject's refresh tokens at the identity
	// provider when HMS decides a credential is no longer valid, so GIP
	// agrees with the HMS watermark instead of quietly disagreeing
	// (#781). platform must not import a module, so this stays a narrow
	// capability exactly like Tokens above; the revocation watermark
	// checker itself reaches the iam module through its own constructor,
	// not through Deps — see iam.New. Set by main.go; nil in tests that
	// do not exercise it.
	TokenRevoker authn.TokenRevoker
	// Reconcile ensures a tenant's permission tuples match the registry.
	// Set by main.go; nil in tests that do not exercise it.
	Reconcile func(ctx context.Context, tenantID string) error
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
	Consumers(deps Deps) []events.Consumer
	// Broadcasts declares this module's fanout subscriptions — every
	// replica hears every message, unlike Consumers, where replicas
	// sharing a durable name compete and exactly one wins (#781). Most
	// modules return nil; iam uses it to invalidate its revocation cache
	// the instant another replica revokes a credential.
	Broadcasts(deps Deps) []events.Broadcast
}
