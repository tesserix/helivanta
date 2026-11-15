package iam

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PermGovernanceManage authz.Permission = "iam.governance.manage"
const PermGovernanceRead authz.Permission = "iam.governance.read"
const PermGovernanceReview authz.Permission = "iam.governance.review"

//go:embed governance.sql
var governanceSchema string

var errGovernanceConflict = errors.New("governance command changed")
var errGovernanceDenied = errors.New("governance authority denied")

type governanceHandlers struct {
	db       *tenantdb.DB
	bus      *events.Bus
	identity MembershipIdentityReader
	topology authz.CurrentGovernanceTopologyGuard
}

type governanceRow struct {
	ID                          uuid.UUID
	InputHash, IssuedBy, Status string
	Version                     int64
	Receipt                     json.RawMessage
	UpdatedAt                   time.Time
}

type governanceNoticeRow struct {
	GovernanceOwnerNotice `gorm:"embedded"`
}

func (r governanceNoticeRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (m *Module) registerGovernance(g *platform.Router, deps platform.Deps) {
	h := &governanceHandlers{db: deps.DB, bus: deps.Bus, identity: m.membershipIdentity, topology: deps.GovernanceTopology}
	g.PUT("/governance-grants/:id", PermGovernanceManage, h.put)
	g.GET("/governance-grants/:id", PermGovernanceRead, h.get)
	g.PUT("/governance-grants/:id/decisions/:command", PermGovernanceReview, h.decide)
	platform.ListRoute(g, "/governance-notices", PermGovernanceRead, h.notices)
}

func governancePrincipal(c *gin.Context, permission authz.Permission) (authn.Principal, bool) {
	if !authn.Refresh(c) || !authz.Refresh(c, permission) {
		return authn.Principal{}, false
	}
	p, _, ok := membershipCommandPrincipal(c)
	return p, ok
}

func governanceError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errGovernanceInput):
		respond.Error(c, 400, "invalid_request", "Provide one bounded governance command with a fixed template and exact scope.")
	case errors.Is(err, errGovernanceConflict), errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, 409, "governance_conflict", "This command or its source changed. Reconcile the saved receipt before retrying.")
	case errors.Is(err, errGovernanceDenied), errors.Is(err, errRevocationActorNotAdmin):
		respond.Forbidden(c, "Current governance authority and a separate recipient are required.")
	case errors.Is(err, authz.ErrGovernanceScopeHidden), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "governance grant or scope")
	case errors.Is(err, errGrantIdentityIneligible):
		respond.Error(c, 409, "identity_ineligible", "Choose an active verified human account owned by this workspace.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "Sign in again before managing governance.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, errMembershipAuthority), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(err, tenantdb.ErrGuardedCommandExpired):
		respond.Error(c, 503, "governance_unavailable", "Current governance authority or its source is unavailable. Retry the same command.")
	default:
		respond.InternalErr(c, errors.New("governance storage operation failed"), "Could not process the governance command.")
	}
}

func (h *governanceHandlers) withActor(ctx context.Context, p authn.Principal, action func(context.Context, *gorm.DB, []string) error) error {
	return h.withActorPrelude(ctx, p, nil, action)
}

func (h *governanceHandlers) withActorPrelude(ctx context.Context, p authn.Principal, prelude func(*gorm.DB) error, action func(context.Context, *gorm.DB, []string) error) error {
	if h.db == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) {
		return authz.ErrScopedCredentialRevoked
	}
	until := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(until) {
		until = p.IdleDeadline
	}
	ctx = tenantdb.WithActor(ctx, p.Subject)
	var owners []string
	return h.db.WithGuardedTenantCommand(ctx, p.TenantID, until, func(tx *gorm.DB) error {
		if prelude != nil {
			if err := prelude(tx); err != nil {
				return err
			}
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.governance_owners:"+p.TenantID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.credential:"+p.Subject).Error; err != nil {
			return err
		}
		var revoked bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, p.Subject, p.AuthTime).Scan(&revoked).Error; err != nil {
			return err
		}
		if revoked {
			return authz.ErrScopedCredentialRevoked
		}
		if err := tx.Table("iam_members").Where("tenant_id=? AND role_key='tenant_admin'", p.TenantID).Order("subject").Limit(1001).Pluck("subject", &owners).Error; err != nil {
			return err
		}
		if len(owners) > 1000 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.iam_governance_access','',true)`, p.Subject).Error
	}, func(commandCtx context.Context) error {
		return h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error { return action(commandCtx, tx, owners) })
	})
}

func (h *governanceHandlers) withOwner(ctx context.Context, p authn.Principal, action func(context.Context, *gorm.DB, []string) error) error {
	if action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.withActor(ctx, p, func(commandCtx context.Context, tx *gorm.DB, owners []string) error {
		if !slices.Contains(owners, p.Subject) {
			return errGovernanceDenied
		}
		if err := tx.Exec(`SELECT set_config('app.iam_governance_access','owner',true)`).Error; err != nil {
			return err
		}
		return action(commandCtx, tx, owners)
	})
}

func loadGovernance(tx *gorm.DB, tenant string, id uuid.UUID) (governanceRow, error) {
	var row governanceRow
	err := tx.Table("iam_governance_grants").Select("id,input_hash,issued_by,status,version,receipt,updated_at").Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error
	return row, err
}

func governanceOriginal(row governanceRow, actor, hash string) (GovernanceGrantReceipt, error) {
	if row.IssuedBy != actor || row.InputHash != hash {
		return GovernanceGrantReceipt{}, errGovernanceConflict
	}
	var receipt GovernanceGrantReceipt
	err := json.Unmarshal(row.Receipt, &receipt)
	return receipt, err
}

func (h *governanceHandlers) put(c *gin.Context) {
	p, ok := governancePrincipal(c, PermGovernanceManage)
	if !ok {
		return
	}
	id, ok := staffAccountID(c.Param("id"))
	if !ok {
		respond.NotFound(c, "governance command")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, governanceMaxRequestBytes))
	if err != nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		governanceError(c, errGovernanceInput)
		return
	}
	input, err := DecodeGovernanceGrant(raw)
	if err != nil {
		governanceError(c, err)
		return
	}
	if p.Subject == input.Subject {
		governanceError(c, errGovernanceDenied)
		return
	}
	input.ValidFrom, input.ExpiresAt = input.ValidFrom.UTC(), input.ExpiresAt.UTC()
	raw, err = json.Marshal(input)
	if err != nil {
		governanceError(c, err)
		return
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var receipt GovernanceGrantReceipt
	found := false
	err = h.withOwner(c.Request.Context(), p, func(_ context.Context, tx *gorm.DB, _ []string) error {
		row, err := loadGovernance(tx, p.TenantID, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		receipt, err = governanceOriginal(row, p.Subject, hash)
		if err != nil {
			return err
		}
		found = true
		return governanceAudit(tx, p, id, "iam.governance.receipt.read")
	})
	if err != nil {
		governanceError(c, err)
		return
	}
	if found {
		c.JSON(http.StatusAccepted, receipt)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err = requireNewGrantIdentity(ctx, h.identity, p.TenantID, input.Subject); err != nil {
		governanceError(c, err)
		return
	}
	err = h.withOwner(ctx, p, func(commandCtx context.Context, tx *gorm.DB, owners []string) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.governance_grant:"+p.TenantID+":"+id.String()).Error; err != nil {
			return err
		}
		row, err := loadGovernance(tx, p.TenantID, id)
		if err == nil {
			receipt, err = governanceOriginal(row, p.Subject, hash)
			if err != nil {
				return err
			}
			return governanceAudit(tx, p, id, "iam.governance.receipt.read")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if h.topology == nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		for _, owner := range owners {
			if owner == input.Subject {
				return errGovernanceDenied
			}
		}
		var members []member
		if err := tx.Select("id,subject,role_key").Where("tenant_id=? AND subject=?", p.TenantID, input.Subject).Order("role_key").Limit(len(authz.SystemRoles()) + 1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&members).Error; err != nil {
			return err
		}
		if len(members) == 0 {
			return gorm.ErrRecordNotFound
		}
		if len(members) > len(authz.SystemRoles()) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, input.Subject)
		if err != nil {
			return err
		}
		if epoch != input.ExpectedMembershipEpoch {
			return errGovernanceConflict
		}
		scope := authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: input.ScopeKind, ID: input.ScopeID}
		return h.topology.WithGovernanceTopology(commandCtx, scope, func(_ context.Context, topology authz.GovernanceTopologyReference) error {
			if topology.HospitalCount > 1 && len(owners) < 2 {
				return errGovernanceDenied
			}
			var now time.Time
			if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
				return err
			}
			if !input.ExpiresAt.After(now) {
				return errGovernanceConflict
			}
			receipt = GovernanceGrantReceipt{ID: id, TenantID: uuid.MustParse(p.TenantID), Input: input, Status: "pending_review", Version: 1, IssuedBy: p.Subject, OwnerCount: int64(len(owners)), AuthorizationMode: "independent_review", CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
			if len(owners) == 1 {
				receipt.Status = "provisional_active"
				receipt.AuthorizationMode = "single_owner_provisional"
			}
			return h.create(tx, p, receipt, raw, hash, owners)
		})
	})
	if err != nil {
		governanceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, receipt)
}

func governanceAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,'iam_governance_grants',?)`, p.TenantID, p.Subject, action, id).Error
}

func (h *governanceHandlers) create(tx *gorm.DB, p authn.Principal, receipt GovernanceGrantReceipt, input []byte, hash string, owners []string) error {
	if h.bus == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if err := tx.Exec(`SELECT set_config('app.iam_governance_command','grant',true)`).Error; err != nil {
		return err
	}
	in := receipt.Input
	if err := tx.Exec(`INSERT INTO iam_governance_grants(tenant_id,id,subject,membership_epoch,template_key,scope_kind,scope_id,input,input_hash,status,version,issued_by,owner_count,authorization_mode,valid_from,expires_at,receipt,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?::jsonb,?,?,1,?,?,?,?,?,?::jsonb,?,?)`, p.TenantID, receipt.ID, in.Subject, in.ExpectedMembershipEpoch, in.TemplateKey, in.ScopeKind, in.ScopeID, string(input), hash, receipt.Status, p.Subject, receipt.OwnerCount, receipt.AuthorizationMode, in.ValidFrom, in.ExpiresAt, string(encoded), receipt.CreatedAt, receipt.UpdatedAt).Error; err != nil {
		return err
	}
	for _, owner := range owners {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_governance_owner_notices(tenant_id,id,grant_id,grant_version,recipient_subject,version,created_at,updated_at)VALUES(?,?,?,1,?,1,?,?)`, p.TenantID, id, receipt.ID, owner, receipt.CreatedAt, receipt.CreatedAt).Error; err != nil {
			return err
		}
	}
	if err := governanceAudit(tx, p, receipt.ID, "iam.governance.granted"); err != nil {
		return err
	}
	if receipt.Status != "provisional_active" {
		return nil
	}
	roleID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO iam_members(tenant_id,id,subject,role_key)VALUES(?,?,?,?)ON CONFLICT(tenant_id,subject,role_key)DO NOTHING`, p.TenantID, roleID, in.Subject, authz.RoleGovernanceDelegate).Error; err != nil {
		return err
	}
	data, err := json.Marshal(iamcontract.MemberChangedData{Subject: in.Subject, RoleKey: string(authz.RoleGovernanceDelegate)})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, iamcontract.SubjectMemberGranted, events.Event{Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *governanceHandlers) get(c *gin.Context) {
	p, ok := governancePrincipal(c, PermGovernanceRead)
	if !ok {
		return
	}
	id, ok := staffAccountID(c.Param("id"))
	if !ok {
		respond.NotFound(c, "governance grant")
		return
	}
	var receipt GovernanceGrantReceipt
	err := h.withReviewer(c.Request.Context(), p, id, false, PermGovernanceRead, func(_ context.Context, tx *gorm.DB, _ []string, _ governanceReviewAuthority) error {
		row, err := loadGovernance(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(row.Receipt, &receipt); err != nil {
			return err
		}
		receipt.Status, receipt.Version, receipt.UpdatedAt = row.Status, row.Version, row.UpdatedAt.UTC()
		return governanceAudit(tx, p, id, "iam.governance.read")
	})
	if err != nil {
		governanceError(c, err)
		return
	}
	respond.OK(c, receipt)
}

func (h *governanceHandlers) notices(c *gin.Context, page pagination.Params) ([]governanceNoticeRow, error) {
	p, ok := governancePrincipal(c, PermGovernanceRead)
	if !ok {
		return nil, nil
	}
	var rows []governanceNoticeRow
	err := h.withOwner(c.Request.Context(), p, func(_ context.Context, tx *gorm.DB, _ []string) error {
		query := tx.Table("iam_governance_owner_notices").Select("id,tenant_id,grant_id,grant_version,recipient_subject,version,created_at,seen_at").Where("tenant_id=? AND recipient_subject=?", p.TenantID, p.Subject)
		if err := tenantdb.ApplyKeyset(query, page).Find(&rows).Error; err != nil {
			return err
		}
		return governanceAudit(tx, p, uuid.MustParse(p.TenantID), "iam.governance.notices.read")
	})
	if err != nil {
		governanceError(c, err)
		return nil, nil
	}
	return rows, nil
}
