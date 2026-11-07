package authz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

// OperatingAssignment is an immutable nonclinical IAM graph revision, never current operating authority.
type OperatingAssignment struct {
	TenantID, EmployerKind, EmployerID, LocationID, ID, Subject          string
	Version, MembershipEpoch, SourceVersion, LocationVersion, ScopeEpoch int64
	Identity                                                             IndependentStaffReference
	ValidFrom, ExpiresAt                                                 time.Time
	Permissions                                                          []Permission
}

func (a OperatingAssignment) objectKey() (string, error) {
	if a.EmployerKind != "supplier" && a.EmployerKind != "pharmacy" {
		return "", ErrInvalidScopedAssignment
	}
	for _, value := range []string{a.TenantID, a.EmployerID, a.LocationID, a.ID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return "", ErrInvalidScopedAssignment
		}
	}
	for _, value := range []string{a.EmployerID, a.LocationID, a.ID} {
		id, _ := uuid.Parse(value)
		if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return "", ErrInvalidScopedAssignment
		}
	}
	for _, version := range []int64{a.Version, a.MembershipEpoch, a.SourceVersion, a.LocationVersion, a.ScopeEpoch, a.Identity.StaffVersion} {
		if version < 1 || version > 9007199254740991 {
			return "", ErrInvalidScopedAssignment
		}
	}
	if a.Identity.StaffID == uuid.Nil || a.Identity.StaffID.Variant() != uuid.RFC4122 || a.Identity.LinkID.Version() != 7 || a.Identity.LinkID.Variant() != uuid.RFC4122 || a.Identity.LinkVersion < 2 || a.Identity.LinkVersion > 9007199254740991 || !validAssignmentSubject(a.Subject) || a.ValidFrom.IsZero() || !a.ExpiresAt.After(a.ValidFrom) {
		return "", ErrInvalidScopedAssignment
	}
	if len(a.Permissions) < 1 || len(a.Permissions) > 32 {
		return "", ErrInvalidScopedAssignment
	}
	a.Permissions = slices.Clone(a.Permissions)
	slices.Sort(a.Permissions)
	for i, p := range a.Permissions {
		if len(p) > 100 || !scopedPermissionPattern.MatchString(string(p)) || (i > 0 && p == a.Permissions[i-1]) {
			return "", ErrInvalidScopedAssignment
		}
	}
	a.ValidFrom = a.ValidFrom.UTC()
	a.ExpiresAt = a.ExpiresAt.UTC()
	content, err := json.Marshal(a)
	if err != nil {
		return "", ErrInvalidScopedAssignment
	}
	digest := sha256.Sum256(append([]byte("helivanta.iam.operating-assignment.v1\n"), content...))
	return fmt.Sprintf("%s/op_%x", a.TenantID, digest), nil
}

// ProjectOperatingAssignment cannot make the snapshot effective; IAM must acknowledge the current revision.
func (c *Client) ProjectOperatingAssignment(ctx context.Context, a OperatingAssignment) error {
	key, err := a.objectKey()
	if err != nil {
		return err
	}
	object := "assignment:" + key
	for _, edge := range []struct{ user, relation string }{{TenantObject(a.TenantID), "tenant"}, {userObject(a.Subject), "subject"}} {
		if err := c.write(ctx, edge.user, edge.relation, object); err != nil {
			return ErrScopedAuthorizationUnavailable
		}
	}
	for _, permission := range a.Permissions {
		if err := c.write(ctx, object, "granted_assignment", "scoped_permission:"+key+"/"+string(permission)); err != nil {
			return ErrScopedAuthorizationUnavailable
		}
	}
	return nil
}

// CheckOperatingAssignment checks only the graph; current owner, IAM, identity, time and eligibility checks remain mandatory.
func (c *Client) CheckOperatingAssignment(ctx context.Context, a OperatingAssignment, actor string, permission Permission) (bool, error) {
	key, err := a.objectKey()
	if err != nil || !validAssignmentSubject(actor) {
		return false, ErrInvalidScopedAssignment
	}
	if !slices.Contains(a.Permissions, permission) {
		return false, nil
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	result, err := c.api.Check(ctx).Body(fgaclient.ClientCheckRequest{User: userObject(actor), Relation: "can_do", Object: "scoped_permission:" + key + "/" + string(permission)}).Options(fgaclient.ClientCheckOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return false, ErrScopedAuthorizationUnavailable
	}
	return result.GetAllowed(), nil
}

type OperatingAssignmentChecker interface {
	CheckOperatingAssignment(context.Context, OperatingAssignment, string, Permission) (bool, error)
}
