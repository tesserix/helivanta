package supply

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type partySummary struct {
	ID               uuid.UUID       `json:"id"`
	Code             string          `json:"code"`
	RecordKind       string          `json:"record_kind"`
	LegalName        string          `json:"legal_name"`
	TradingName      string          `json:"trading_name"`
	BusinessKind     string          `json:"business_kind"`
	CountryCode      string          `json:"country_code"`
	OnboardingStatus string          `json:"onboarding_status"`
	AccessRoles      json.RawMessage `json:"access_roles" gorm:"type:jsonb"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (p partySummary) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

func (h *handlers) listParties(c *gin.Context, params pagination.Params) ([]partySummary, error) {
	p, ok := principal(c)
	if !ok {
		return nil, nil
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if len(q) > 200 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "supplier search exceeds 200 bytes")
		return nil, nil
	}
	var rows []partySummary
	e := h.transaction(c.Request.Context(), p, uuid.Nil, true, has(c, PermAccessManage), func(tx *gorm.DB) error {
		query := tenantdb.ApplyKeyset(tx, params).Table("supply_parties").Select(`id,code,record_kind,legal_name,trading_name,business_kind,country_code,onboarding_status,created_at,
   COALESCE((SELECT jsonb_agg(a.role ORDER BY a.role) FROM supply_party_access a WHERE a.tenant_id=supply_parties.tenant_id AND a.party_id=supply_parties.id AND a.subject=?),'[]'::jsonb) AS access_roles`, p.Subject).
			Where("tenant_id=?", p.TenantID)
		if q != "" {
			prefix := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
			query = query.Where("(lower(code) LIKE ? OR lower(legal_name) LIKE ? OR lower(trading_name) LIKE ?)", prefix, prefix, prefix)
		}
		return query.Find(&rows).Error
	})
	if e != nil {
		supplierError(c, e)
	}
	return rows, nil
}
