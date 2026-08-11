package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

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
// written. Safe to call from every replica at boot.
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

func (c *Client) ensureStore(ctx context.Context, name string) (string, error) {
	stores, err := c.api.ListStores(ctx).Execute()
	if err != nil {
		return "", fmt.Errorf("list stores: %w", err)
	}
	for _, s := range stores.GetStores() {
		if s.GetName() == name {
			return s.GetId(), nil
		}
	}
	created, err := c.api.CreateStore(ctx).
		Body(fgaclient.ClientCreateStoreRequest{Name: name}).Execute()
	if err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}
	return created.GetId(), nil
}

// ensureModel writes modelJSON unless a model already exists. The model
// is immutable in practice, so a store that has one is already correct.
func (c *Client) ensureModel(ctx context.Context) error {
	existing, err := c.api.ReadAuthorizationModels(ctx).Execute()
	if err != nil {
		return fmt.Errorf("read models: %w", err)
	}
	if len(existing.GetAuthorizationModels()) > 0 {
		return nil
	}
	var body fgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &body); err != nil {
		return fmt.Errorf("parse model: %w", err)
	}
	if _, err := c.api.WriteAuthorizationModel(ctx).Body(body).Execute(); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	return nil
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
func (c *Client) Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error) {
	res, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{
		User:     userObject(subject),
		Relation: "can_do",
		Type:     "perm",
	}).Execute()
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	prefix := "perm:" + tenantID + "/"
	set := PermissionSet{}
	for _, obj := range res.GetObjects() {
		if rest, ok := strings.CutPrefix(obj, prefix); ok {
			set[Permission(rest)] = struct{}{}
		}
	}
	return set, nil
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
