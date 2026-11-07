package authz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
)

type PartnerAssignment struct {
	Grant           supplieraccess.SupplierGrant `json:"grant"`
	Subject         string                       `json:"subject"`
	MembershipEpoch int64                        `json:"membership_epoch"`
	RecipientHash   string                       `json:"recipient_hash"`
}

func (a PartnerAssignment) ProjectionHash() (string, error) {
	if supplieraccess.ValidateRecord(a.Grant) != nil || a.Grant.Side != "source" || a.Grant.Status != "approved" || !validAssignmentSubject(a.Subject) || a.MembershipEpoch < 1 || a.MembershipEpoch > 9007199254740991 || len(a.RecipientHash) != 64 || strings.ToLower(a.RecipientHash) != a.RecipientHash {
		return "", ErrInvalidScopedAssignment
	}
	if _, err := hex.DecodeString(a.RecipientHash); err != nil {
		return "", ErrInvalidScopedAssignment
	}
	if a.Grant.Input.RecipientKind == "subject" && (a.Grant.Input.RecipientSubject == nil || *a.Grant.Input.RecipientSubject != a.Subject) {
		return "", ErrInvalidScopedAssignment
	}
	a.Grant.CreatedAt = a.Grant.CreatedAt.UTC()
	a.Grant.UpdatedAt = a.Grant.UpdatedAt.UTC()
	a.Grant.Input.ValidFrom = a.Grant.Input.ValidFrom.UTC()
	a.Grant.Input.ExpiresAt = a.Grant.Input.ExpiresAt.UTC()
	raw, err := json.Marshal(a)
	if err != nil {
		return "", ErrInvalidScopedAssignment
	}
	sum := sha256.Sum256(append([]byte("helivanta.iam.partner-assignment.v1\n"), raw...))
	return hex.EncodeToString(sum[:]), nil
}
func (a PartnerAssignment) objectKey() (string, error) {
	digest, err := a.ProjectionHash()
	if err != nil {
		return "", err
	}
	return a.Grant.Scope.SourceTenantID.String() + "/partner_" + digest, nil
}
func (c *Client) ProjectPartnerAssignment(ctx context.Context, a PartnerAssignment) error {
	key, err := a.objectKey()
	if err != nil {
		return err
	}
	template, ok := supplieraccess.TemplateFor(a.Grant.Input.TemplateKey)
	if !ok {
		return ErrInvalidScopedAssignment
	}
	object := "assignment:" + key
	for _, edge := range []struct{ user, relation string }{{TenantObject(a.Grant.Scope.RecipientTenantID.String()), "tenant"}, {userObject(a.Subject), "subject"}} {
		if err := c.write(ctx, edge.user, edge.relation, object); err != nil {
			return ErrScopedAuthorizationUnavailable
		}
	}
	if err := c.write(ctx, object, "granted_assignment", "scoped_permission:"+key+"/"+template.Permission); err != nil {
		return ErrScopedAuthorizationUnavailable
	}
	return nil
}
func (c *Client) CheckPartnerAssignment(ctx context.Context, a PartnerAssignment, actor string, permission Permission) (bool, error) {
	key, err := a.objectKey()
	if err != nil || !validAssignmentSubject(actor) {
		return false, ErrInvalidScopedAssignment
	}
	template, ok := supplieraccess.TemplateFor(a.Grant.Input.TemplateKey)
	if !ok {
		return false, ErrInvalidScopedAssignment
	}
	if template.Permission != string(permission) {
		return false, nil
	}
	consistency := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	result, err := c.api.Check(ctx).Body(fgaclient.ClientCheckRequest{User: userObject(actor), Relation: "can_do", Object: "scoped_permission:" + key + "/" + string(permission)}).Options(fgaclient.ClientCheckOptions{Consistency: &consistency}).Execute()
	if err != nil {
		return false, ErrScopedAuthorizationUnavailable
	}
	return result.GetAllowed(), nil
}
