package customeronboarding

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// NewReceipt opens a separate signup without changing any existing owner record.
func (h *Handlers) NewReceipt(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	if !decode(c, new(struct{})) {
		return
	}
	next, err := newCapability()
	if err != nil {
		unavailable(c)
		return
	}
	setCapability(c, next)
	c.JSON(http.StatusOK, customercontract.Receipt{State: customercontract.NotStarted})
}
