package pharmacy

import "github.com/tesserix/helivanta/pkg/tenantdb"

func stocktakeSnapshotIndexes() []tenantdb.ConcurrentIndex {
	return []tenantdb.ConcurrentIndex{{
		Name: "pharmacy_stock_batch_snapshot", Table: "pharmacy_stock_batches",
		Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "outlet_id"}, {Name: "product_id"}, {Name: "id"}},
	}}
}
