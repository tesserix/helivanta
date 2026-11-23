package pharmacy

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func stocktakeWorkScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	hospitalValue, departmentValue := c.Param("hospital"), c.Param("department")
	if hospitalValue == "" && departmentValue == "" {
		return p, pharmacy, outlet, true
	}
	hospital, err := uuid.Parse(hospitalValue)
	_, departmentOK := operatingStockID(departmentValue)
	if err != nil || hospital == uuid.Nil || hospital.Variant() != uuid.RFC4122 || hospital.String() != hospitalValue || !departmentOK {
		respond.NotFound(c, "pharmacy resource")
		return p, uuid.Nil, uuid.Nil, false
	}
	return p, pharmacy, outlet, true
}
