package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

// openFGADefaultListObjectsMaxResults is OpenFGA's default cap on the
// number of objects a single ListObjects call returns (server flag
// --listObjects-max-results, default 1000; see OpenFGA's ListObjects
// docs). ListObjects has no cursor/pagination — a caller that hits the
// cap has no way to fetch the rest in the same call. Resolve and
// ListRoles both call ListObjects, so both warn when a result reaches
// this count.
const openFGADefaultListObjectsMaxResults = 1000

// listObjectsTruncated reports whether n, the number of objects a
// ListObjects call returned, hit the server's result cap — OpenFGA's
// only signal that more objects may exist beyond what was returned.
// Kept as a pure predicate, separate from the slog call site, so the
// threshold can be unit-tested without spinning up an OpenFGA container
// and writing 1000+ tuples into it.
func listObjectsTruncated(n int) bool {
	return n >= openFGADefaultListObjectsMaxResults
}

// Client is the OpenFGA decision point. Every write helper is
// idempotent, because the iam-fga-sync consumer retries on failure and
// may redeliver.
type Client struct {
	api *fgaclient.OpenFgaClient
}

// RoleObject and PermObject namespace every object by tenant, which is
// what keeps ListObjects results tenant-scoped.
func RoleObject(tenantID string, r Role) string {
	return fmt.Sprintf("role:%s/%s", tenantID, r)
}

func PermObject(tenantID string, p Permission) string {
	return fmt.Sprintf("perm:%s/%s", tenantID, p)
}

func userObject(subject string) string { return "user:" + subject }

// NewClient connects to OpenFGA, reusing the named store if it exists
// and creating it otherwise, then ensures the authorization model is
// written.
//
// Store selection is deterministic across replicas: OpenFGA does not
// enforce store-name uniqueness, so replicas racing to boot for the
// first time can each create a same-named store. ensureStore always
// resolves to the lexicographically smallest store ID sharing the name
// (OpenFGA store IDs are ULIDs, so this is also the oldest store),
// re-listing after its own CreateStore call rather than trusting the ID
// it just created. Every replica therefore converges on the same store
// even when a duplicate was transiently created, so a grant written
// through one replica is always visible to Resolve through another. The
// residual race window is narrow and self-healing: two replicas can
// both pass the initial "not found" check and both call CreateStore,
// but every subsequent selectStore call (by those replicas or any
// other) re-lists and deterministically picks the same winner, so the
// split never persists past that first boot.
func NewClient(ctx context.Context, apiURL, storeName string) (*Client, error) {
	api, err := fgaclient.NewSdkClient(&fgaclient.ClientConfiguration{ApiUrl: apiURL})
	if err != nil {
		return nil, fmt.Errorf("openfga client: %w", err)
	}
	c := &Client{api: api}

	storeID, err := c.ensureStore(ctx, storeName)
	if err != nil {
		return nil, err
	}
	if err := api.SetStoreId(storeID); err != nil {
		return nil, fmt.Errorf("set store id: %w", err)
	}
	if err := c.ensureModel(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// ensureStore returns the ID of the store named name, creating it if no
// store by that name exists yet. See NewClient's doc comment for why
// the returned ID always comes from selectStore rather than directly
// from CreateStore's response.
func (c *Client) ensureStore(ctx context.Context, name string) (string, error) {
	if id, ok, err := c.selectStore(ctx, name); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}

	if _, err := c.api.CreateStore(ctx).
		Body(fgaclient.ClientCreateStoreRequest{Name: name}).Execute(); err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}

	// Re-list instead of trusting the ID just created: a concurrent
	// replica may have created (or be about to create) a same-named
	// store, and every replica must land on the same winner.
	id, ok, err := c.selectStore(ctx, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("openfga store %q not found immediately after create", name)
	}
	return id, nil
}

// selectStore returns the deterministic winner among every store named
// name, fetched fresh from the API. The selection rule itself lives in
// smallestStoreID so it can be unit-tested without a running OpenFGA
// container.
func (c *Client) selectStore(ctx context.Context, name string) (string, bool, error) {
	stores, err := c.api.ListStores(ctx).Execute()
	if err != nil {
		return "", false, fmt.Errorf("list stores: %w", err)
	}
	id, ok := smallestStoreID(stores.GetStores(), name)
	return id, ok, nil
}

// smallestStoreID returns the lexicographically smallest ID among the
// stores named name, or ok=false if none match. OpenFGA store IDs are
// ULIDs, which sort lexicographically in creation order, so this always
// picks the oldest store and therefore the same store on every replica
// and every call, regardless of how many same-named duplicates a
// creation race left behind and regardless of the order the API
// happened to return them in. Kept as a pure function, independent of
// *Client, so the selection rule can be exercised directly against
// hand-built input rather than only through an integration test whose
// ListStores order can coincidentally match the rule under test.
func smallestStoreID(stores []openfga.Store, name string) (id string, ok bool) {
	for _, s := range stores {
		if s.GetName() != name {
			continue
		}
		if !ok || s.GetId() < id {
			id, ok = s.GetId(), true
		}
	}
	return id, ok
}

// ensureModel writes modelJSON unless the store's latest model already
// has the same type definitions.
//
// The previous implementation returned early whenever ANY model
// existed, on the reasoning that the model is immutable in practice.
// That stopped being true when the tenant type was added: every store
// created before it would otherwise keep a model with no `member`
// relation forever, and every membership check against such a store
// fails — which locks out every user in every tenant. So this compares
// rather than assumes.
//
// OpenFGA models are immutable and append-only: writing produces a new
// model id rather than mutating the old one, so an upgrade never
// invalidates tuples. Racing replicas may both write; both then re-read
// and converge on the same latest id, exactly as ensureStore does for
// the store id and for the same reason.
func (c *Client) ensureModel(ctx context.Context) error {
	var desired fgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &desired); err != nil {
		return fmt.Errorf("parse model: %w", err)
	}

	existing, err := c.api.ReadAuthorizationModels(ctx).Execute()
	if err != nil {
		return fmt.Errorf("read models: %w", err)
	}
	if models := existing.GetAuthorizationModels(); len(models) > 0 {
		// ReadAuthorizationModels returns newest first.
		if sameTypeDefinitions(models[0].GetTypeDefinitions(), desired.TypeDefinitions) {
			return nil
		}
		slog.InfoContext(ctx, "authorization model differs from desired; writing a new version",
			"existing_model_id", models[0].GetId())
	}
	if _, err := c.api.WriteAuthorizationModel(ctx).Body(desired).Execute(); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	return nil
}

// sameTypeDefinitions compares two models by the set of type names and
// each type's relation names.
//
// It deliberately does not compare the full rewrite trees. A structural
// deep-compare of OpenFGA's generated types is brittle across SDK
// versions, and being wrong in the "they differ" direction is cheap:
// the model is re-written, which is idempotent in effect because
// OpenFGA versions rather than mutates. Being wrong in the "they match"
// direction is the expensive one, and adding a type or a relation —
// the only changes this repo has ever made — always changes a name.
func sameTypeDefinitions(existing, desired []openfga.TypeDefinition) bool {
	shape := func(defs []openfga.TypeDefinition) map[string][]string {
		out := make(map[string][]string, len(defs))
		for _, d := range defs {
			rels := make([]string, 0, len(d.GetRelations()))
			for name := range d.GetRelations() {
				rels = append(rels, name)
			}
			sort.Strings(rels)
			out[d.GetType()] = rels
		}
		return out
	}
	return reflect.DeepEqual(shape(existing), shape(desired))
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.ReadAuthorizationModels(ctx).Execute(); err != nil {
		return fmt.Errorf("openfga ping: %w", err)
	}
	return nil
}

// Resolve returns every permission the subject holds in the tenant, in
// exactly one FGA call. Any error returns an error and never a partial
// set, so callers can fail closed unambiguously.
//
// The underlying ListObjects call is capped by OpenFGA at
// openFGADefaultListObjectsMaxResults objects, with no cursor to fetch
// the rest — so this is NOT guaranteed complete for a subject holding an
// extreme number of permissions in one tenant. Resolve logs a warning
// when the raw result hits the cap, but cannot itself detect which
// entries (if any) are missing.
func (c *Client) Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error) {
	res, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{
		User:     userObject(subject),
		Relation: "can_do",
		Type:     "perm",
	}).Execute()
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	objects := res.GetObjects()
	if listObjectsTruncated(len(objects)) {
		slog.WarnContext(ctx, "openfga ListObjects result hit the result cap; permissions may be truncated",
			"caller", "Resolve", "tenant_id", tenantID, "subject", subject, "count", len(objects))
	}
	prefix := "perm:" + tenantID + "/"
	set := PermissionSet{}
	for _, obj := range objects {
		if rest, ok := strings.CutPrefix(obj, prefix); ok {
			set[Permission(rest)] = struct{}{}
		}
	}
	return set, nil
}

// RoleBinding is one tenant/role pair a subject holds, as resolved from
// OpenFGA's role assignments rather than any Postgres table. Membership
// is proved by resolution: iam_members is a write-side record used to
// derive tuples, but OpenFGA is the read-side source of truth for "what
// tenants does this subject belong to", because it is the only store
// that can be queried without first knowing which tenant to scope to.
type RoleBinding struct {
	TenantID string
	Role     Role
}

// ListRoles returns every role binding subject holds, across every
// tenant, in exactly one FGA call. Like Resolve, it returns an error and
// never a partial slice on failure, so callers can fail closed
// unambiguously; unlike Resolve it is not tenant-scoped, because the
// caller does not yet know which tenant(s) to ask about — that is the
// question this method answers. Objects that do not parse as
// "role:<tenantID>/<roleKey>" are skipped rather than erroring, since a
// malformed object would otherwise take down an unrelated lookup. The
// result is sorted by tenant then role so responses built from it are
// deterministic.
func (c *Client) ListRoles(ctx context.Context, subject string) ([]RoleBinding, error) {
	res, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{
		User:     userObject(subject),
		Relation: "assignee",
		Type:     "role",
	}).Execute()
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	objects := res.GetObjects()
	if listObjectsTruncated(len(objects)) {
		slog.WarnContext(ctx, "openfga ListObjects result hit the result cap; role bindings may be truncated",
			"caller", "ListRoles", "subject", subject, "count", len(objects))
	}
	bindings := make([]RoleBinding, 0, len(objects))
	for _, obj := range objects {
		rest, ok := strings.CutPrefix(obj, "role:")
		if !ok {
			continue
		}
		tenantID, roleKey, ok := strings.Cut(rest, "/")
		if !ok || tenantID == "" || roleKey == "" {
			continue
		}
		bindings = append(bindings, RoleBinding{TenantID: tenantID, Role: Role(roleKey)})
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].TenantID != bindings[j].TenantID {
			return bindings[i].TenantID < bindings[j].TenantID
		}
		return bindings[i].Role < bindings[j].Role
	})
	return bindings, nil
}

func (c *Client) GrantRole(ctx context.Context, tenantID, subject string, role Role) error {
	return c.write(ctx, userObject(subject), "assignee", RoleObject(tenantID, role))
}

func (c *Client) RevokeRole(ctx context.Context, tenantID, subject string, role Role) error {
	return c.delete(ctx, userObject(subject), "assignee", RoleObject(tenantID, role))
}

func (c *Client) GrantPermission(ctx context.Context, tenantID string, perm Permission, role Role) error {
	return c.write(ctx, RoleObject(tenantID, role), "granted_role", PermObject(tenantID, perm))
}

// write is idempotent: OpenFGA rejects a duplicate tuple with a 400
// validation error coded write_failed_due_to_invalid_input, which we
// swallow so retried consumer deliveries succeed.
func (c *Client) write(ctx context.Context, user, relation, object string) error {
	_, err := c.api.Write(ctx).Body(fgaclient.ClientWriteRequest{
		Writes: []fgaclient.ClientTupleKey{{User: user, Relation: relation, Object: object}},
	}).Execute()
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("write tuple %s#%s@%s: %w", object, relation, user, err)
	}
	return nil
}

// delete is idempotent for the same reason as write: OpenFGA rejects
// deleting a tuple that isn't there with the same validation error
// code, distinguished here by message so we don't swallow unrelated
// invalid-input errors.
func (c *Client) delete(ctx context.Context, user, relation, object string) error {
	_, err := c.api.Write(ctx).Body(fgaclient.ClientWriteRequest{
		Deletes: []fgaclient.ClientTupleKeyWithoutCondition{{User: user, Relation: relation, Object: object}},
	}).Execute()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete tuple %s#%s@%s: %w", object, relation, user, err)
	}
	return nil
}

// isAlreadyExists reports whether err is OpenFGA's validation error for
// writing a tuple that already exists. The SDK surfaces this as a typed
// FgaApiValidationError with a stable error code; we additionally check
// the message so we never swallow an unrelated invalid-input error that
// happens to share the same code.
func isAlreadyExists(err error) bool {
	return isWriteFailedValidationError(err) && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

// isNotFound reports whether err is OpenFGA's validation error for
// deleting a tuple that does not exist. See isAlreadyExists for why the
// error code alone is not a sufficient discriminator.
func isNotFound(err error) bool {
	return isWriteFailedValidationError(err) && strings.Contains(strings.ToLower(err.Error()), "does not exist")
}

// isWriteFailedValidationError reports whether err is the typed OpenFGA
// validation error the API returns for both "tuple already exists" and
// "tuple does not exist" write failures.
func isWriteFailedValidationError(err error) bool {
	var validationErr openfga.FgaApiValidationError
	if !errors.As(err, &validationErr) {
		return false
	}
	return validationErr.ResponseCode() == openfga.ERRORCODE_WRITE_FAILED_DUE_TO_INVALID_INPUT
}
