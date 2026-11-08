package supply

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type orderProductsGuard struct {
	db       *tenantdb.DB
	products stockproduct.Guard
}

// SupplierOrderProducts exposes current published product facts for warehouse order review.
func (*Module) SupplierOrderProducts(db *tenantdb.DB, products stockproduct.Guard) supplierorder.CurrentOrderProducts {
	return &orderProductsGuard{db: db, products: products}
}
func (g *orderProductsGuard) WithCurrentOrderProducts(ctx context.Context, e suppliertrade.Endpoint, lines []supplierorder.SupplierOrderLine, action func(context.Context) error) error {
	if g.db == nil || g.products == nil || action == nil || !g.db.GuardedCommandActive(ctx) {
		return supplierorder.ErrUnavailable
	}
	scope := authz.OperatingLocationScope{TenantID: e.TenantID.String(), EmployerKind: "supplier", EmployerID: e.OrganizationID, LocationID: e.LocationID}
	if e.Product != "supply" || !authz.ValidOperatingLocationScope(scope) {
		return supplierorder.ErrIneligible
	}
	parent, err := (&operatingLocationGuard{db: g.db}).CurrentOperatingLocation(ctx, scope)
	if err != nil {
		return err
	}
	if parent.Scope != scope || parent.EmployerVersion != e.OrganizationVersion || parent.LocationVersion != e.LocationVersion || parent.Country != "IN" {
		return supplierorder.ErrIneligible
	}
	if supplierorder.ValidateProductLines(lines, e.Product) != nil {
		return supplierorder.ErrInvalid
	}
	guard, ok := g.products.(stockproduct.CommandGuard)
	if !ok {
		return supplierorder.ErrUnavailable
	}
	ordered := slices.Clone(lines)
	slices.SortFunc(ordered, func(a, b supplierorder.SupplierOrderLine) int {
		return strings.Compare(a.Product.VersionID.String(), b.Product.VersionID.String())
	})
	// Each check leaves its publication fence on the outer command; avoid nested system transactions.
	for _, line := range ordered {
		err = guard.WithCurrentStockProductInCommand(ctx, e.TenantID.String(), *line.Product.VersionID, func(_ context.Context, ref stockproduct.Reference) error {
			if ref.ProductID != line.Product.ProductID || ref.UnitOfIssue != line.UnitCode {
				return supplierorder.ErrIneligible
			}
			return nil
		})
		if err != nil {
			break
		}
	}

	if errors.Is(err, stockproduct.ErrIneligible) {
		return supplierorder.ErrIneligible
	}
	if errors.Is(err, stockproduct.ErrUnavailable) {
		return supplierorder.ErrUnavailable
	}
	if err != nil {
		return err
	}
	return action(ctx)
}
