package pharmacy

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func decodeStocktake(c *gin.Context, target any) bool {
	const limit = 64 << 10
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || c.Request.URL.RawQuery != "" || suppliertrade.DecodeBoundedObject(raw, target, limit) != nil || binding.Validator.ValidateStruct(target) != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Provide one bounded stocktake command without query parameters.")
		return false
	}
	return true
}

func stocktakeRecordID(id uuid.UUID) bool {
	return id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func stocktakeText(value string, max int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func normalizeStocktakeStart(in *StocktakeStartInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	if !stocktakeRecordID(in.QuarantineLocationID) || !stocktakeText(in.Reason, 500) || in.ProductIds == nil || len(in.ProductIds) > 100 {
		return false
	}
	if in.Selection != "all" && in.Selection != "selected" || in.Selection == "all" && len(in.ProductIds) != 0 || in.Selection == "selected" && len(in.ProductIds) == 0 {
		return false
	}
	seen := make(map[uuid.UUID]bool, len(in.ProductIds))
	for _, id := range in.ProductIds {
		if !stocktakeRecordID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	slices.SortFunc(in.ProductIds, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return true
}

func normalizeStocktakeCount(in *StocktakeCountInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	if !stocktakeText(in.Reason, 500) || in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || *in.ExpectedVersion > 9007199254740990 || len(in.Counts) < 1 || len(in.Counts) > 100 {
		return false
	}
	seen := make(map[uuid.UUID]bool, len(in.Counts))
	for _, line := range in.Counts {
		if !stocktakeRecordID(line.LineID) || seen[line.LineID] || line.CountedUnits == nil || *line.CountedUnits < 0 || *line.CountedUnits > 1000000000000 {
			return false
		}
		seen[line.LineID] = true
	}
	slices.SortFunc(in.Counts, func(a, b StocktakeCountEntry) int { return bytes.Compare(a.LineID[:], b.LineID[:]) })
	return true
}
