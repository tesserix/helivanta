package patientmaster

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func decodePatientInput(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(raw) {
		respond.BadRequest(c, errors.New("invalid request body"))
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		respond.BadRequest(c, errors.New("invalid request body"))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.BadRequest(c, errors.New("expected one JSON object"))
		return false
	}
	if err := binding.Validator.ValidateStruct(value); err != nil {
		respond.BadRequest(c, errors.New("invalid request fields"))
		return false
	}
	return true
}

func (h *patientHandlers) search(c *gin.Context, params pagination.Params) ([]patient, error) {
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var input struct {
		Query string `json:"query" binding:"max=100"`
	}
	if !decodePatientInput(c, &input) {
		return nil, nil
	}
	query := strings.TrimSpace(input.Query)
	var rows []patient
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, false, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital)
		if query != "" {
			// Escape LIKE metacharacters; the user supplies a literal prefix, not a pattern.
			prefix := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(matching.NormalizeName(query)) + "%"
			q = q.Where("normalized_name LIKE ? OR mrn = ? OR mobile = ?", prefix, strings.ToUpper(query), query)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return patientAudit(tx, p, hospital, "list", rows)
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}
