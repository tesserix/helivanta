package iam

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type revocationHandlers struct {
	db      *tenantdb.DB
	bus     *events.Bus
	checker *RevocationChecker
	roles   platform.RoleLister
}

// signOut ends every session for the calling subject, on every device.
//
// HMS session tokens carry no session identifier, so revocation is
// necessarily by subject — see the design spec's D5 (the #838 Zitadel
// design; the original decision predates it under GIP but the reasoning
// is unchanged: neither identity provider's token format is asked to
// carry one). That is the correct answer for the shared-workstation case
// that motivates this work: a ward terminal's previous user must not be
// recoverable.
func (h *revocationHandlers) signOut(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	if err := h.revoke(c, p.Subject, time.Now().UTC(), "sign_out", p.Subject); err != nil {
		respond.InternalErr(c, err, "could not sign out")
		return
	}
	respond.OK(c, gin.H{"signed_out": true})
}

// adminRevoke lets a tenant admin cut off a compromised account
// immediately.
//
// The target must be a member of the acting admin's tenant. Without
// that gate any tenant admin could revoke any subject on the platform.
// A non-member is answered 404, this codebase's cross-tenant answer —
// 403 would confirm the subject exists somewhere.
//
// The effect is nonetheless cross-tenant by construction: a credential
// is global, so revoking it ends that subject's sessions everywhere.
// That is deliberate — forcing a hospital to leave a known-compromised
// credential alive elsewhere would be worse — and it is why every
// revoke is logged with actor, target and tenant.
func (h *revocationHandlers) adminRevoke(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	target := c.Param("subject")

	bindings, err := h.roles.ListRoles(c.Request.Context(), target)
	if err != nil {
		respondRolesUnavailable(c, err)
		return
	}
	if !hasBindingForTenant(bindings, p.TenantID) {
		respond.NotFound(c, "subject")
		return
	}
	if err := h.revoke(c, target, time.Now().UTC(), "admin_revoke", p.Subject); err != nil {
		respond.InternalErr(c, err, "could not revoke credentials")
		return
	}
	requestid.Logger(c).WarnContext(c.Request.Context(), "credentials revoked by administrator",
		"target_subject", target, "actor_subject", p.Subject, "tenant_id", p.TenantID,
		"cross_tenant_effect", "the target's sessions end in every tenant, not only this one")
	respond.OK(c, gin.H{"revoked": true, "subject": target})
}

// revoke writes the watermark and publishes the invalidation in ONE
// transaction. Splitting them would allow a watermark that propagates
// only at TTL, or an invalidation for a revocation that never happened.
//
// This is the WHOLE of revocation, post-#838: the HMS watermark is
// authoritative and there is no separate upstream call to make.
// HMS stores no Zitadel credential to revoke (spec D4a — renewal is the
// login exchange re-run with a fresh Zitadel token, not a server-side
// refresh of a stored one), so unlike the old GIP-backed design there is
// nothing left to "tell" after the commit. A subject whose watermark
// moves is refused by authn.Middleware on their very next request,
// regardless of what Zitadel still believes.
func (h *revocationHandlers) revoke(c *gin.Context, subject string, at time.Time, reason, actor string) error {
	err := h.db.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
		if err := h.checker.RevokeTx(tx, subject, at, reason, actor); err != nil {
			return err
		}
		data, err := json.Marshal(iamcontract.CredentialRevokedData{Subject: subject})
		if err != nil {
			return err
		}
		// TenantID is deliberately left empty: a revocation is
		// subject-scoped, not tenant-scoped (spec D3) — it ends every
		// session for this subject in every tenant. events.Event.TenantID
		// is not required to be set; the bus only uses it, when present
		// and a valid UUID, to scope the consuming transaction's RLS GUC
		// (pkg/events/bus.go), which this event has no need of because
		// iam_credential_revocations carries no tenant_id at all.
		return h.bus.Publish(tx, iamcontract.SubjectCredentialRevoked, events.Event{
			Type: "CredentialRevoked", Version: 1, Data: data,
		})
	})
	if err != nil {
		return err
	}
	// Drop our own entry immediately rather than waiting for our own
	// broadcast: the replica that served this request should never serve
	// the revoked credential again, not even for one more request.
	h.checker.Invalidate(subject)
	return nil
}
