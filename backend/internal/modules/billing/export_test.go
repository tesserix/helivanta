package billing

import "time"

// Tests apply setup at once and may drive small census batches.
func init() { censusSettle = 0 }

// SetCensusBatch sets the census batch and pass budget for a test.
func SetCensusBatch(batch int, budget time.Duration) (restore func()) {
	oldBatch, oldBudget := censusBatch, censusBudget
	censusBatch, censusBudget = batch, budget
	return func() { censusBatch, censusBudget = oldBatch, oldBudget }
}
