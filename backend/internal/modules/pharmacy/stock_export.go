package pharmacy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/csvsafe"
	"github.com/tesserix/helivanta/pkg/pagination"
)

const stockExportRowLimit = 1000

func (h *catalogueHandlers) exportStockPositions(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	filters, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid stock export filters")
		return
	}
	for key, values := range filters {
		switch key {
		case "as_of", "location_id", "product_id", "batch_id", "expiry", "balance":
		default:
			respond.Error(c, http.StatusBadRequest, "invalid_request", "unsupported stock export filter")
			return
		}
		if len(values) != 1 || values[0] == "" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "stock export filters must be nonempty and occur once")
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	rows, err := h.listStockPositions(c, pagination.Params{Limit: stockExportRowLimit})
	if err != nil {
		stockError(c, err)
		return
	}
	if c.IsAborted() {
		return
	}
	if len(rows) > stockExportRowLimit {
		respond.Error(c, http.StatusRequestEntityTooLarge, "stock_export_too_large", "stock export exceeds 1000 rows; narrow the filters")
		return
	}
	asOf, _ := time.Parse(time.RFC3339Nano, c.Query("as_of"))
	data, err := encodeStockCSV(rows, asOf)
	if errors.Is(err, csvsafe.ErrTooLarge) {
		respond.Error(c, http.StatusRequestEntityTooLarge, "stock_export_too_large", "stock export exceeds 8 MiB; narrow the filters")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not encode stock CSV")
		return
	}
	if err := ctx.Err(); err != nil {
		stockError(c, err)
		return
	}

	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.stock.exported", "stock CSV generated", "tenant_id", p.TenantID, "subject", p.Subject, "pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet, "as_of", asOf.UTC(), "row_count", len(rows), "sha256", digest)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-stock-%s-%s.csv"`, outlet, asOf.UTC().Format("20060102t150405z")))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(len(rows)))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
