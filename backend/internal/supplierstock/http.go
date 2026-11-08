package supplierstock

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

// Adapter keeps every physical decision and operating/hospital admission in its owning module.
type Adapter interface {
	Admit(context.Context, authn.Principal, f.SourceScope, func(context.Context, supplierorder.Actor) error) error
	Snapshot(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error)
	Lock(*gorm.DB, f.Allocation) error
	Apply(*gorm.DB, f.AllocationCommandReceipt) error
}
type HTTP struct {
	Owner                            Owner
	DB                               *tenantdb.DB
	Orders                           f.CurrentOrderGuard
	Adapter                          Adapter
	OrganizationParam, LocationParam string
}

func (h HTTP) Register(r *platform.Router, base string) {
	r.PUT(base, authz.Public, h.create)
	r.GET(base, authz.Public, h.current)
	r.GET(base+"/creation-receipt", authz.Public, h.original)
	r.PUT(base+"/commands/:command", authz.Public, h.command)
	r.GET(base+"/commands/:command", authz.Public, h.recover)
}
func id(c *gin.Context, name string) (uuid.UUID, bool) {
	raw := c.Param(name)
	v, e := uuid.Parse(raw)
	if e != nil || v.Version() != 7 || v.Variant() != uuid.RFC4122 || v.String() != raw {
		respond.NotFound(c, "supplier fulfilment resource")
		return uuid.Nil, false
	}
	return v, true
}
func (h HTTP) request(c *gin.Context) (authn.Principal, f.SourceScope, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, f.SourceScope{}, uuid.Nil, false
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, f.SourceScope{}, uuid.Nil, false
	}
	for name, want := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		v := c.Request.Header.Values(name)
		if len(v) != 1 || v[0] != want {
			respond.Error(c, 409, "fulfilment_context_changed", "The workspace or account changed. Refresh before continuing.")
			return p, f.SourceScope{}, uuid.Nil, false
		}
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("fulfilment commands do not accept query parameters"))
		return p, f.SourceScope{}, uuid.Nil, false
	}
	org, ok := id(c, h.OrganizationParam)
	if !ok {
		return p, f.SourceScope{}, uuid.Nil, false
	}
	loc, ok := id(c, h.LocationParam)
	if !ok {
		return p, f.SourceScope{}, uuid.Nil, false
	}
	order, ok := id(c, "order")
	if !ok {
		return p, f.SourceScope{}, uuid.Nil, false
	}
	allocation, ok := id(c, "allocation")
	if !ok {
		return p, f.SourceScope{}, uuid.Nil, false
	}
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil {
		respond.NotFound(c, "supplier fulfilment resource")
		return p, f.SourceScope{}, uuid.Nil, false
	}
	scope := f.SourceScope{TenantID: tenant, OrderID: order, OrganizationID: org, LocationID: loc, Product: string(h.Owner)}
	if c.Param("hospital") != "" {
		hospital, err := uuid.Parse(c.Param("hospital"))
		if err != nil || hospital == uuid.Nil || hospital.Variant() != uuid.RFC4122 || hospital.String() != c.Param("hospital") {
			respond.NotFound(c, "hospital stock source")
			return p, scope, uuid.Nil, false
		}
		dept, ok := id(c, "department")
		if !ok {
			return p, scope, uuid.Nil, false
		}
		scope.HospitalID = hospital
		scope.DepartmentID = dept
	}
	return p, scope, allocation, true
}
func decode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || suppliertrade.DecodeObject(raw, target) != nil {
		respond.BadRequest(c, errors.New("invalid fulfilment intent"))
		return false
	}
	return true
}
func Error(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credential_revoked", "Sign in again.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, supplierorder.ErrIneligible), errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, authz.ErrDepartmentReferenceChanged):
		respond.NotFound(c, "supplier fulfilment resource")
	case errors.Is(err, f.ErrConflict), errors.Is(err, supplierorder.ErrConflict), errors.Is(err, suppliertrade.ErrConflict), errors.Is(err, suppliertrade.ErrIneligible):
		respond.Error(c, 409, "fulfilment_changed", "The order, stock, reservation or source evidence changed. Refresh and recover the original command.")
	case errors.Is(err, f.ErrInvalid), errors.Is(err, supplierorder.ErrInvalid), errors.Is(err, suppliertrade.ErrInvalid):
		respond.BadRequest(c, errors.New("invalid fulfilment intent"))
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Current fulfilment authority is required")
	case errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514"):
		respond.Error(c, 409, "fulfilment_changed", "The physical stock or command state changed. Refresh the owner evidence.")
	default:
		respond.Error(c, 503, "fulfilment_unavailable", "Current authority or owner evidence could not be confirmed. No new action is confirmed.")
	}
}
func (h HTTP) with(c *gin.Context, p authn.Principal, s f.SourceScope, allocation uuid.UUID, action func(context.Context, *gorm.DB, supplierorder.Actor) error) error {
	if h.DB == nil || h.Adapter == nil || h.Orders == nil {
		return f.ErrUnavailable
	}
	ctx := tenantdb.WithActor(c.Request.Context(), p.Subject)
	return h.Adapter.Admit(ctx, p, s, func(ctx context.Context, a supplierorder.Actor) error {
		return h.DB.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
			return (Store{Owner: h.Owner}).Frame(tx, Scope{TenantID: s.TenantID, OrganizationID: s.OrganizationID, LocationID: s.LocationID, AllocationID: allocation, OrderID: s.OrderID, StaffID: a.StaffID, Subject: a.Subject, Mode: "operator"}, func() error { return action(ctx, tx, a) })
		})
	})
}
func matches(a f.Allocation, s f.SourceScope) bool {
	return a.Plan.OwnerTenantID == s.TenantID && a.Plan.OrderID == s.OrderID && a.Plan.SourceOrganizationID == s.OrganizationID && a.Plan.SourceLocationID == s.LocationID && a.Plan.Product.Owner == s.Product
}
func (h HTTP) current(c *gin.Context) {
	p, s, id, ok := h.request(c)
	if !ok {
		return
	}
	var view f.AllocationView
	err := h.with(c, p, s, id, func(_ context.Context, tx *gorm.DB, _ supplierorder.Actor) error {
		a, err := (Store{Owner: h.Owner}).Current(tx, s.TenantID, id)
		if err != nil {
			return err
		}
		if !matches(a, s) {
			return gorm.ErrRecordNotFound
		}
		at, err := Clock(tx)
		if err != nil {
			return err
		}
		view, err = f.Observe(a, at)
		return err
	})
	if err != nil {
		Error(c, err)
		return
	}
	respond.OK(c, view)
}
func (h HTTP) original(c *gin.Context) {
	p, s, id, ok := h.request(c)
	if !ok {
		return
	}
	var result f.Allocation
	err := h.with(c, p, s, id, func(_ context.Context, tx *gorm.DB, a supplierorder.Actor) error {
		row, err := (Store{Owner: h.Owner}).Current(tx, s.TenantID, id)
		if err != nil {
			return err
		}
		if !matches(row, s) {
			return gorm.ErrRecordNotFound
		}
		result, err = (Store{Owner: h.Owner}).Original(tx, s.TenantID, id, row.Plan.Input, a)
		return err
	})
	if err != nil {
		Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h HTTP) create(c *gin.Context) {
	p, s, id, ok := h.request(c)
	if !ok {
		return
	}
	var in f.ReserveInput
	if !decode(c, &in) {
		return
	}
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	store := Store{Owner: h.Owner}
	var result f.Allocation
	recoverOriginal := func() error {
		return h.with(c, p, s, id, func(_ context.Context, tx *gorm.DB, a supplierorder.Actor) error {
			if err := store.AllocationLock(tx, s.TenantID, id); err != nil {
				return err
			}
			v, err := store.Original(tx, s.TenantID, id, in, a)
			if err == nil && !matches(v, s) {
				return gorm.ErrRecordNotFound
			}
			result = v
			return err
		})
	}
	if err := recoverOriginal(); err == nil {
		respond.OK(c, result)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		Error(c, err)
		return
	}
	proof, err := h.Orders.VerifyOrder(tenantdb.WithActor(c.Request.Context(), p.Subject), s, f.NewReservation)
	if err == nil {
		err = h.with(c, p, s, id, func(ctx context.Context, tx *gorm.DB, a supplierorder.Actor) error {
			return proof.WithCurrent(ctx, func(ctx context.Context, order supplierorder.SupplierOrder) error {
				v, recovered, e := store.Create(tx, id, order, in, a, h.Adapter.Snapshot)
				if e != nil {
					return e
				}
				if !recovered {
					if e = h.DB.ConstrainGuardedCommand(ctx, p.TenantID, v.Plan.Input.ExpiresAt); e != nil {
						return e
					}
				}
				result = v
				return nil
			})
		})
	}
	if err != nil {
		if recovered := recoverOriginal(); recovered == nil {
			respond.OK(c, result)
			return
		}
		Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h HTTP) recover(c *gin.Context) {
	p, s, allocation, ok := h.request(c)
	if !ok {
		return
	}
	command, ok := id(c, "command")
	if !ok {
		return
	}
	var result f.AllocationCommandReceipt
	err := h.with(c, p, s, allocation, func(_ context.Context, tx *gorm.DB, a supplierorder.Actor) error {
		row, e := (Store{Owner: h.Owner}).Current(tx, s.TenantID, allocation)
		if e != nil {
			return e
		}
		if !matches(row, s) {
			return gorm.ErrRecordNotFound
		}
		result, e = (Store{Owner: h.Owner}).Recover(tx, s.TenantID, allocation, command, a, nil)
		return e
	})
	if err != nil {
		Error(c, err)
		return
	}
	respond.OK(c, result)
}
func (h HTTP) command(c *gin.Context) {
	p, s, allocation, ok := h.request(c)
	if !ok {
		return
	}
	command, ok := id(c, "command")
	if !ok {
		return
	}
	var in f.AllocationDecisionInput
	if !decode(c, &in) {
		return
	}
	store := Store{Owner: h.Owner}
	var result f.AllocationCommandReceipt
	recoverOriginal := func() error {
		return h.with(c, p, s, allocation, func(_ context.Context, tx *gorm.DB, a supplierorder.Actor) error {
			row, e := store.Current(tx, s.TenantID, allocation)
			if e != nil {
				return e
			}
			if !matches(row, s) {
				return gorm.ErrRecordNotFound
			}
			if e = store.CommandLock(tx, s.TenantID, command); e != nil {
				return e
			}
			result, e = store.Recover(tx, s.TenantID, allocation, command, a, &in)
			return e
		})
	}
	if err := recoverOriginal(); err == nil {
		respond.OK(c, result)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		Error(c, err)
		return
	}
	purpose := f.DispatchOrder
	if in.Decision == "release" {
		purpose = f.RetainedOrder
	} else if in.Decision != "dispatch" {
		Error(c, f.ErrInvalid)
		return
	}
	proof, err := h.Orders.VerifyOrder(tenantdb.WithActor(c.Request.Context(), p.Subject), s, purpose)
	if err == nil {
		err = h.with(c, p, s, allocation, func(ctx context.Context, tx *gorm.DB, a supplierorder.Actor) error {
			return proof.WithCurrent(ctx, func(ctx context.Context, order supplierorder.SupplierOrder) error {
				prior, e := store.Current(tx, s.TenantID, allocation)
				if e != nil {
					return e
				}
				if !matches(prior, s) || prior.Plan.OrderInputSha256 != order.InputSha256 {
					return f.ErrConflict
				}
				result, _, e = store.Command(tx, s.TenantID, allocation, command, in, a, h.Adapter)
				if e != nil {
					return e
				}
				if in.Decision == "dispatch" {
					return h.DB.ConstrainGuardedCommand(ctx, p.TenantID, result.Allocation.Plan.Input.ExpiresAt)
				}
				return nil
			})
		})
	}
	if err != nil {
		if recovered := recoverOriginal(); recovered == nil {
			respond.OK(c, result)
			return
		}
		Error(c, err)
		return
	}
	respond.OK(c, result)
}
