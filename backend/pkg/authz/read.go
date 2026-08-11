package authz

import (
	"context"
	"fmt"
	"strings"

	fgaclient "github.com/openfga/go-sdk/client"
)

// Tuple is one stored authorization tuple, exactly as OpenFGA holds it.
// It exists so the reconciler can compare what OpenFGA contains against
// what Postgres backs, and delete the difference — the only way a revoke
// that was lost in flight ever converges. Compare/hash it through Key,
// never with ==, so a future field can be added without silently
// changing what "the same tuple" means.
type Tuple struct {
	User     string
	Relation string
	Object   string
}

// Key is the identity of a tuple as OpenFGA sees it: a tuple is the
// (user, relation, object) triple and nothing else. Used as a map key
// for set membership on both sides of the reconcile comparison.
func (t Tuple) Key() string {
	return t.User + "#" + t.Relation + "@" + t.Object
}

// reconciledObjectTypes are the object types the reconciler owns end to
// end, and therefore the only types it is ever allowed to delete from.
// Both are namespaced "<type>:<tenantID>/<name>" (see model.go), which
// is what makes tenant-scoped pruning possible at all. Any type added to
// the model later is invisible to ReadTuplesByTenant until it is listed
// here, so a new object type can never be pruned by a reconciler that
// does not yet know how to derive its desired state.
var reconciledObjectTypes = []string{"role:", "perm:"}

// isReconciledObject reports whether object is of a type the reconciler
// owns. Read returns every tuple in the store — it filters by object
// type or exact object, and OpenFGA rejects a type-only filter with an
// empty object id and empty user — so the type gate lives here.
func isReconciledObject(object string) bool {
	for _, prefix := range reconciledObjectTypes {
		if strings.HasPrefix(object, prefix) {
			return true
		}
	}
	return false
}

// readPageSize is the per-call page size for the Read API. Read, unlike
// ListObjects, has a continuation token, so this is a round-trip/latency
// knob and never a correctness cap.
const readPageSize = 100

// readMaxPages bounds the pagination loop. A server that kept returning
// a non-empty continuation token forever would otherwise spin a boot
// sequence indefinitely; failing loudly at a page count far above any
// realistic store is strictly better than hanging.
const readMaxPages = 100_000

// TenantOfObject returns the tenant an object of a reconciled type is
// namespaced to, i.e. "tenantA" for "role:tenantA/doctor". ok is false
// for anything that does not parse as "<type>:<tenantID>/<name>", so a
// malformed or foreign-shaped object is never attributed to a tenant —
// and therefore never lands in a bucket the reconciler would delete
// from.
//
// Exported because tenant isolation for deletes depends on it: the
// reconciler re-derives the tenant from every object it is about to
// delete and refuses to delete one that does not belong to the tenant
// whose desired state it just computed.
func TenantOfObject(object string) (string, bool) {
	_, rest, ok := strings.Cut(object, ":")
	if !ok {
		return "", false
	}
	tenantID, name, ok := strings.Cut(rest, "/")
	if !ok || tenantID == "" || name == "" {
		return "", false
	}
	return tenantID, true
}

// ReadTuplesByTenant returns every role: and perm: tuple in the store,
// bucketed by the tenant its object is namespaced to. Tuples whose
// object does not parse (see TenantOfObject) are dropped rather than
// bucketed, so an object the reconciler does not understand can never be
// deleted by it.
//
// Bucketing here rather than at the call site is deliberate: it means a
// caller physically cannot obtain a mixed-tenant slice, so the tenant
// scoping of any subsequent delete is a property of this API and not of
// each caller remembering to filter.
//
// One whole-store pass, not one pass per tenant: OpenFGA's Read has no
// object-id prefix filter — and rejects a type-only filter outright,
// requiring a non-empty object id or user whenever a tuple_key is sent
// at all — so the store is read unfiltered and both the type gate and
// the tenant bucketing happen here. Doing it in one pass is O(tuples)
// rather than the O(tenants x tuples) a per-tenant read would cost. This
// runs at boot, and the result is three strings per tuple.
//
// An error returns immediately with no partial map: callers must treat a
// failed read as "unknown", never as "empty", because an empty
// existing-set would make every backed tuple look orphaned.
func (c *Client) ReadTuplesByTenant(ctx context.Context) (map[string][]Tuple, error) {
	out := map[string][]Tuple{}
	pageSize := int32(readPageSize)
	var token string
	for page := 0; page < readMaxPages; page++ {
		opts := fgaclient.ClientReadOptions{PageSize: &pageSize}
		if token != "" {
			opts.ContinuationToken = &token
		}
		// An empty body means "every tuple": the SDK omits tuple_key
		// entirely unless at least one of its fields is set.
		res, err := c.api.Read(ctx).Body(fgaclient.ClientReadRequest{}).Options(opts).Execute()
		if err != nil {
			return nil, fmt.Errorf("read tuples: %w", err)
		}
		for _, t := range res.GetTuples() {
			key := t.GetKey()
			tenantID, ok := TenantOfObject(key.Object)
			if !ok || !isReconciledObject(key.Object) {
				continue
			}
			out[tenantID] = append(out[tenantID], Tuple{
				User: key.User, Relation: key.Relation, Object: key.Object,
			})
		}
		token = res.GetContinuationToken()
		if token == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("read tuples: continuation token still set after %d pages", readMaxPages)
}

// DeleteTuple removes one tuple. Idempotent for the same reason every
// other write helper is: deleting a tuple that is already gone is not an
// error, so a retried or concurrent reconcile converges instead of
// failing.
func (c *Client) DeleteTuple(ctx context.Context, t Tuple) error {
	return c.delete(ctx, t.User, t.Relation, t.Object)
}
