package pharmacy

import "github.com/tesserix/helivanta/pkg/tenantdb"

func stocktakeSnapshotIndexes() []tenantdb.ConcurrentIndex {
	return []tenantdb.ConcurrentIndex{{
		Name: "pharmacy_stock_batch_identity", Table: "pharmacy_stock_batches",
		Columns: []tenantdb.IndexColumn{{Name: "id"}},
	}}
}
