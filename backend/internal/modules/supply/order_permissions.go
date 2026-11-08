package supply

import "github.com/tesserix/helivanta/pkg/authz"

const (
	PermPurchaseOrderRead    authz.Permission = "supply.purchase_order.read"
	PermPurchaseOrderPrepare authz.Permission = "supply.purchase_order.prepare"
	PermPurchaseOrderReview  authz.Permission = "supply.purchase_order.review"
	PermPurchaseOrderCancel  authz.Permission = "supply.purchase_order.cancel"
	PermSupplierOrderRead    authz.Permission = "supply.supplier_order.read"
	PermSupplierOrderAccept  authz.Permission = "supply.supplier_order.accept"
)
