package authz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

// ScopedAssignment is an immutable IAM projection snapshot, not proof of approval or current validity.
type ScopedAssignment struct {
	TenantID    string
	HospitalID  string
	ScopeKind   string
	ScopeID     string
	ID          string
	Version     int64
	Subject     string
	Permissions []Permission
}

var (
	ErrInvalidScopedAssignment        = errors.New("invalid scoped assignment")
	ErrScopedAuthorizationUnavailable = errors.New("scoped authorization unavailable")
	scopedPermissionPattern           = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){2,5}$`)
)

func (a ScopedAssignment) objectKey() (string, error) {
	for _, value := range []string{a.TenantID, a.HospitalID, a.ScopeID, a.ID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return "", ErrInvalidScopedAssignment
		}
	}
	id, _ := uuid.Parse(a.ID)
	if id.Version() != 7 || a.Version < 1 || (a.ScopeKind != "hospital" && a.ScopeKind != "staff") || (a.ScopeKind == "hospital" && a.ScopeID != a.HospitalID) {
		return "", ErrInvalidScopedAssignment
	}
	if !validAssignmentSubject(a.Subject) || len(a.Permissions) == 0 || len(a.Permissions) > 32 {
		return "", ErrInvalidScopedAssignment
	}
	permissions := slices.Clone(a.Permissions)
	slices.Sort(permissions)
	for i, p := range permissions {
		if len(p) > 100 || !scopedPermissionPattern.MatchString(string(p)) || (i > 0 && p == permissions[i-1]) {
			return "", ErrInvalidScopedAssignment
		}
	}
	// Binding the immutable content prevents conflicting or delayed projections from sharing keys.
	a.Permissions = permissions
	content, err := json.Marshal(a)
	if err != nil {
		return "", ErrInvalidScopedAssignment
	}
	digest := sha256.Sum256(content)
	return fmt.Sprintf("%s/%x", a.TenantID, digest), nil
}

func validAssignmentSubject(subject string) bool {
	return utf8.ValidString(subject) && utf8.RuneCountInString(subject) > 0 && utf8.RuneCountInString(subject) <= 200 &&
		!strings.ContainsAny(subject, "*#:\x00") && strings.IndexFunc(subject, unicode.IsSpace) < 0
}

// ProjectScopedAssignment writes an immutable graph revision; IAM must acknowledge its current epoch separately.
func (c *Client) ProjectScopedAssignment(ctx context.Context, a ScopedAssignment) error {
	key, err := a.objectKey()
	if err != nil {
		return err
	}
	assignment := "assignment:" + key
	for _, edge := range []struct{ user, relation string }{
		{TenantObject(a.TenantID), "tenant"},
		{userObject(a.Subject), "subject"},
	} {
		if err := c.write(ctx, edge.user, edge.relation, assignment); err != nil {
			return ErrScopedAuthorizationUnavailable
		}
	}
	for _, p := range a.Permissions {
		if err := c.write(ctx, assignment, "granted_assignment", "scoped_permission:"+key+"/"+string(p)); err != nil {
			return ErrScopedAuthorizationUnavailable
		}
	}
	return nil
}

// CheckScopedAssignment checks only the graph; callers must verify current IAM epoch, validity and owner state before allowing access.
func (c *Client) CheckScopedAssignment(ctx context.Context, a ScopedAssignment, actor string, permission Permission) (bool, error) {
	key, err := a.objectKey()
	if err != nil || !validAssignmentSubject(actor) {
		return false, ErrInvalidScopedAssignment
	}
	if !slices.Contains(a.Permissions, permission) {
		return false, nil
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	result, err := c.api.Check(ctx).Body(fgaclient.ClientCheckRequest{
		User: userObject(actor), Relation: "can_do", Object: "scoped_permission:" + key + "/" + string(permission),
	}).Options(fgaclient.ClientCheckOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return false, ErrScopedAuthorizationUnavailable
	}
	return result.GetAllowed(), nil
}
