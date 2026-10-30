package authz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

var ErrInvalidMembership = errors.New("invalid membership projection")
var ErrMembershipUnavailable = errors.New("membership graph authority unavailable")

// ListRoleCandidates is discovery only; IAM must verify current row proofs.
func (c *Client) ListRoleCandidates(ctx context.Context, subject string) ([]RoleBinding, error) {
	if !validAssignmentSubject(subject) {
		return nil, ErrInvalidMembership
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	response, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{User: userObject(subject), Relation: "assignee", Type: "role"}).Options(fgaclient.ClientListObjectsOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return nil, ErrMembershipUnavailable
	}
	objects := response.GetObjects()
	if objects == nil {
		return nil, ErrMembershipUnavailable
	}
	if listObjectsTruncated(len(objects)) {
		return nil, ErrMembershipUnavailable
	}
	var result []RoleBinding
	for _, object := range objects {
		rest, ok := strings.CutPrefix(object, "role:")
		if !ok {
			continue
		}
		tenant, role, ok := strings.Cut(rest, "/")
		id, err := uuid.Parse(tenant)
		if !ok || err != nil || id == uuid.Nil || id.String() != tenant || role == "" {
			continue
		}
		result = append(result, RoleBinding{TenantID: tenant, Role: Role(role)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TenantID != result[j].TenantID {
			return result[i].TenantID < result[j].TenantID
		}
		return result[i].Role < result[j].Role
	})
	return result, nil
}

// Membership binds an immutable IAM row and continuity epoch to one graph principal.
type Membership struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"membership_id"`
	Subject  string `json:"subject"`
	Role     Role   `json:"role_key"`
	Epoch    int64  `json:"membership_epoch"`
}

// RetireMembershipBinding removes edges owned by one observed incarnation only.
func (c *Client) RetireMembershipBinding(ctx context.Context, binding Tuple) error {
	rest, ok := strings.CutPrefix(binding.Object, "membership:")
	if !ok {
		return ErrInvalidMembership
	}
	tenant, digest, ok := strings.Cut(rest, "/")
	decoded, err := hex.DecodeString(digest)
	tenantID, parseErr := uuid.Parse(tenant)
	if !ok || err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) || parseErr != nil || tenantID == uuid.Nil || tenantID.String() != tenant || binding.Relation != "subject" || !strings.HasPrefix(binding.User, "user:") || !validAssignmentSubject(strings.TrimPrefix(binding.User, "user:")) {
		return ErrInvalidMembership
	}
	for _, user := range []string{binding.Object + "#subject", binding.Object} {
		var observed []Tuple
		consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
		token := ""
		relation, object := "assignee", "role:"
		size := int32(100)
		for page := 0; page < 100; page++ {
			options := fgaclient.ClientReadOptions{PageSize: &size, Consistency: &consistency}
			if token != "" {
				options.ContinuationToken = &token
			}
			response, err := c.api.Read(ctx).Body(fgaclient.ClientReadRequest{User: &user, Relation: &relation, Object: &object}).Options(options).Execute()
			if err != nil {
				return ErrMembershipUnavailable
			}
			if response.GetTuples() == nil {
				return ErrMembershipUnavailable
			}
			for _, tuple := range response.GetTuples() {
				key := tuple.GetKey()
				owner, ok := TenantOfObject(key.Object)
				if !ok || owner != tenant || !strings.HasPrefix(key.Object, "role:") || key.Relation != relation || key.User != user {
					return ErrMembershipUnavailable
				}
				observed = append(observed, Tuple{User: key.User, Relation: key.Relation, Object: key.Object})
			}
			token = response.GetContinuationToken()
			if token == "" {
				break
			}
			if page == 99 {
				return ErrMembershipUnavailable
			}
		}
		for _, tuple := range observed {
			if err := c.delete(ctx, tuple.User, tuple.Relation, tuple.Object); err != nil {
				return ErrMembershipUnavailable
			}
		}
	}
	// Preserve the discovery marker until all role edges have been retired.
	if err := c.delete(ctx, binding.User, binding.Relation, binding.Object); err != nil {
		return ErrMembershipUnavailable
	}
	return nil
}

// ReadMembershipBindings supplies observed subject edges for source-based cleanup.
func (c *Client) ReadMembershipBindings(ctx context.Context, subject, tenant string) ([]Tuple, error) {
	if !validAssignmentSubject(subject) {
		return nil, ErrInvalidMembership
	}
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant {
		return nil, ErrInvalidMembership
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	pageSize := int32(100)
	user, relation, object := userObject(subject), "subject", "membership:"
	token := ""
	var bindings []Tuple
	for page := 0; page < 100; page++ {
		options := fgaclient.ClientReadOptions{PageSize: &pageSize, Consistency: &consistency}
		if token != "" {
			options.ContinuationToken = &token
		}
		response, err := c.api.Read(ctx).Body(fgaclient.ClientReadRequest{User: &user, Relation: &relation, Object: &object}).Options(options).Execute()
		if err != nil {
			return nil, ErrMembershipUnavailable
		}
		if response.GetTuples() == nil {
			return nil, ErrMembershipUnavailable
		}
		for _, tuple := range response.GetTuples() {
			key := tuple.GetKey()
			if key.User != userObject(subject) || key.Relation != "subject" {
				return nil, ErrMembershipUnavailable
			}
			if strings.HasPrefix(key.Object, "membership:"+tenant+"/") {
				bindings = append(bindings, Tuple{User: key.User, Relation: key.Relation, Object: key.Object})
			}
		}
		token = response.GetContinuationToken()
		if token == "" {
			return bindings, nil
		}
	}
	return nil, ErrMembershipUnavailable
}

func (m Membership) Object() (string, error) {
	for _, value := range []string{m.TenantID, m.ID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return "", ErrInvalidMembership
		}
	}
	if !validAssignmentSubject(m.Subject) || !KnownRole(m.Role) || m.Epoch < 1 {
		return "", ErrInvalidMembership
	}
	content, err := json.Marshal(m)
	if err != nil {
		return "", ErrInvalidMembership
	}
	digest := sha256.Sum256(content)
	return fmt.Sprintf("membership:%s/%x", m.TenantID, digest), nil
}

// MembershipTuples is the shared projection/reconciliation tuple definition.
func MembershipTuples(m Membership) ([]Tuple, error) {
	object, err := m.Object()
	if err != nil {
		return nil, err
	}
	role := RoleObject(m.TenantID, m.Role)
	return []Tuple{
		{User: role, Relation: "granted_role", Object: TenantObject(m.TenantID)},
		{User: object, Relation: "assignee", Object: role},
		{User: object + "#subject", Relation: "assignee", Object: role},
		{User: userObject(m.Subject), Relation: "subject", Object: object},
	}, nil
}

// ProjectMembership publishes the subject edge after its role edges exist.
func (c *Client) ProjectMembership(ctx context.Context, m Membership) error {
	tuples, err := MembershipTuples(m)
	if err != nil {
		return err
	}
	for _, tuple := range tuples {
		if err := c.write(ctx, tuple.User, tuple.Relation, tuple.Object); err != nil {
			return ErrMembershipUnavailable
		}
	}
	return nil
}

// RevokeMembership retires this incarnation and preserves the shared tenant role.
func (c *Client) RevokeMembership(ctx context.Context, m Membership) error {
	tuples, err := MembershipTuples(m)
	if err != nil {
		return err
	}
	for i := len(tuples) - 1; i > 0; i-- {
		tuple := tuples[i]
		if err := c.delete(ctx, tuple.User, tuple.Relation, tuple.Object); err != nil {
			return ErrMembershipUnavailable
		}
	}
	return nil
}

func (c *Client) IsMembershipReady(ctx context.Context, m Membership) (bool, error) {
	object, err := m.Object()
	if err != nil {
		return false, err
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	roles, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{User: object, Relation: "assignee", Type: "role"}).Options(fgaclient.ClientListObjectsOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return false, ErrMembershipUnavailable
	}
	objects := roles.GetObjects()
	if objects == nil {
		return false, ErrMembershipUnavailable
	}
	if len(objects) == 0 {
		return false, nil
	}
	if len(objects) != 1 || objects[0] != RoleObject(m.TenantID, m.Role) {
		return false, ErrMembershipUnavailable
	}
	checks := []fgaclient.ClientBatchCheckItem{
		{User: userObject(m.Subject), Relation: "subject", Object: object, CorrelationId: "subject"},
		{User: object, Relation: "member", Object: TenantObject(m.TenantID), CorrelationId: "tenant"},
	}
	parallel, batch := int32(1), int32(50)
	response, err := c.api.BatchCheck(ctx).Body(fgaclient.ClientBatchCheckRequest{Checks: checks}).Options(fgaclient.BatchCheckOptions{Consistency: &consistency, MaxParallelRequests: &parallel, MaxBatchSize: &batch}).Execute()
	if err != nil {
		return false, ErrMembershipUnavailable
	}
	results := response.GetResult()
	if len(results) != len(checks) {
		return false, ErrMembershipUnavailable
	}
	ready := true
	for _, check := range checks {
		result, ok := results[check.CorrelationId]
		if !ok || result.HasError() || !result.HasAllowed() {
			return false, ErrMembershipUnavailable
		}
		ready = ready && result.GetAllowed()
	}
	return ready, nil
}

// ResolveMembership never collects permission authority from another incarnation.
func (c *Client) ResolveMembership(ctx context.Context, m Membership) (PermissionSet, error) {
	ready, err := c.IsMembershipReady(ctx, m)
	if err != nil {
		return nil, err
	}
	if !ready {
		return NewPermissionSet(), nil
	}
	object, err := m.Object()
	if err != nil {
		return nil, err
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	response, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{User: object, Relation: "can_do", Type: "perm"}).Options(fgaclient.ClientListObjectsOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return nil, ErrMembershipUnavailable
	}
	if response.GetObjects() == nil || listObjectsTruncated(len(response.GetObjects())) {
		return nil, ErrMembershipUnavailable
	}
	set := NewPermissionSet()
	for _, permission := range response.GetObjects() {
		if value, ok := strings.CutPrefix(permission, "perm:"+m.TenantID+"/"); ok {
			set[Permission(value)] = struct{}{}
		}
	}
	return set, nil
}
