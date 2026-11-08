package supply

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

func TestPurchaseOrderErrorsPreservePublicSemanticsWithoutStorageDisclosure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"revoked credential", authz.ErrScopedCredentialRevoked, 401, "credential_revoked"},
		{"missing order", gorm.ErrRecordNotFound, 404, "not_found"},
		{"ineligible order", supplierorder.ErrIneligible, 404, "not_found"},
		{"ineligible relationship", suppliertrade.ErrIneligible, 404, "not_found"},
		{"hidden department", authz.ErrDepartmentReferenceHidden, 404, "not_found"},
		{"changed department", authz.ErrDepartmentReferenceChanged, 404, "not_found"},
		{"changed order", supplierorder.ErrConflict, 409, "order_changed"},
		{"changed relationship", suppliertrade.ErrConflict, 409, "order_changed"},
		{"invalid order", supplierorder.ErrInvalid, 400, "invalid_request"},
		{"invalid relationship", suppliertrade.ErrInvalid, 400, "invalid_request"},
		{"missing authority", authz.ErrScopedCommandDenied, 403, "order_forbidden"},
		{"duplicate document", &pgconn.PgError{Code: "23505", Message: "private-database-detail"}, 409, "order_changed"},
		{"storage invariant", &pgconn.PgError{Code: "23514", Message: "private-database-detail"}, 409, "order_changed"},
		{"database unavailable", &pgconn.PgError{Code: "08006", Message: "private-database-detail"}, 503, "order_unavailable"},
		{"dependency unavailable", errors.New("private-database-detail"), 503, "order_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			purchaseOrderError(c, fmt.Errorf("private-database-detail: %w", tc.err))
			require.Equal(t, tc.status, response.Code)
			require.Contains(t, response.Body.String(), `"error":"`+tc.code+`"`)
			require.NotContains(t, response.Body.String(), "private-database-detail")
		})
	}
}
