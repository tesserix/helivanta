// Package workspace owns published tenant branding.
package workspace

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const PermBrandingManage authz.Permission = "workspace.branding.manage"

//go:embed schema.sql
var schema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "workspace" }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_workspace", SQL: schema}}
}
func (*Module) Permissions() []authz.Grant                  { return []authz.Grant{{Permission: PermBrandingManage}} }
func (*Module) Publishes() []string                         { return nil }
func (*Module) DirectedSubjects() []string                  { return nil }
func (*Module) DirectedWriteTables() []string               { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := handler{db: deps.DB}
	g := r.Group("/workspace")
	g.GET("/branding", authz.Public, h.get)
	g.PUT("/branding", PermBrandingManage, h.put)
}

type branding struct {
	DisplayName string `json:"display_name"`
	WelcomeText string `json:"welcome_text"`
	LogoURL     string `json:"logo_url"`
	Theme       string `json:"theme"`
	LoginLayout string `json:"login_layout"`
}

func (branding) TableName() string { return "workspace_branding" }
func defaults() branding {
	return branding{DisplayName: "Helivanta", WelcomeText: "A connected workspace for exceptional care.", Theme: "teal", LoginLayout: "split"}
}

type handler struct{ db *tenantdb.DB }

func (h handler) read(ctx context.Context, tenant string) (branding, error) {
	out := defaults()
	err := h.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		err := tx.Select("display_name, welcome_text, logo_url, theme, login_layout").Where("tenant_id = ?", tenant).Take(&out).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	})
	return out, err
}
func (h handler) get(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	h.respond(c, p.TenantID)
}
func (h handler) respond(c *gin.Context, tenant string) {
	c.Header("Cache-Control", "no-store")
	out, err := h.read(c.Request.Context(), tenant)
	if err != nil {
		respond.InternalErr(c, errors.New("branding read failed"), "could not load branding")
		return
	}
	respond.OK(c, out)
}
func validLogo(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" || u.RawQuery != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if net.ParseIP(host) != nil {
		return false
	}
	return true
}
func (h handler) put(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	var in branding
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "invalid branding settings")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "expected one object")
		return
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" || len([]rune(in.DisplayName)) > 100 || len([]rune(in.WelcomeText)) > 240 || len(in.LogoURL) > 1000 || !validLogo(in.LogoURL) ||
		(in.Theme != "teal" && in.Theme != "blue" && in.Theme != "indigo") || (in.LoginLayout != "centered" && in.LoginLayout != "split") {
		respond.Error(c, 400, "invalid_request", "invalid branding settings")
		return
	}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workspace_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workspace_branding(tenant_id,display_name,welcome_text,logo_url,theme,login_layout)
   VALUES (?,?,?,?,?,?) ON CONFLICT(tenant_id) DO UPDATE SET display_name=EXCLUDED.display_name,
   welcome_text=EXCLUDED.welcome_text,logo_url=EXCLUDED.logo_url,theme=EXCLUDED.theme,login_layout=EXCLUDED.login_layout,updated_at=now()
   WHERE (workspace_branding.display_name,workspace_branding.welcome_text,workspace_branding.logo_url,workspace_branding.theme,workspace_branding.login_layout)
   IS DISTINCT FROM (EXCLUDED.display_name,EXCLUDED.welcome_text,EXCLUDED.logo_url,EXCLUDED.theme,EXCLUDED.login_layout)`,
			p.TenantID, in.DisplayName, in.WelcomeText, in.LogoURL, in.Theme, in.LoginLayout).Error
	})
	if err != nil {
		respond.InternalErr(c, errors.New("branding update failed"), "could not save branding")
		return
	}
	respond.OK(c, in)
}

// PublicHandler serves only the deliberately published branding projection.
func PublicHandler(db *tenantdb.DB, limiter ratelimit.Limiter) gin.HandlerFunc {
	if limiter == nil {
		panic("public branding requires a rate limiter")
	}
	return func(c *gin.Context) {
		if !limiter.Allow("branding:"+c.ClientIP(), ratelimit.Rule{Rate: 60, Burst: 20, Per: time.Minute}, time.Now()).Allowed {
			respond.Error(c, 429, "rate_limited", "try again shortly")
			return
		}
		id, err := uuid.Parse(c.Param("tenant"))
		if err != nil || id == uuid.Nil {
			respond.NotFound(c, "branding")
			return
		}
		handler{db: db}.respond(c, id.String())
	}
}
