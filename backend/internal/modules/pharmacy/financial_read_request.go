package pharmacy

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

func pharmacyFinancialCursorRead(c *gin.Context) bool {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	valid := err == nil && c.Request.ContentLength <= 0 && len(c.Request.TransferEncoding) == 0
	for key, values := range query {
		valid = valid && (key == "limit" || key == "cursor") && len(values) == 1 && values[0] != ""
	}
	if !valid {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use only bounded financial pagination parameters and no body.")
	}
	return valid
}
