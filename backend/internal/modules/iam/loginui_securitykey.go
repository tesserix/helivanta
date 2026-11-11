package iam

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
)

type securityKeyRequiredResponse struct {
	SecurityKey struct {
		URL string `json:"url"`
	} `json:"security_key_required"`
}

func (h *LoginUIHandlers) securityKeyContinuation(c *gin.Context, requestID string, s loginclient.Session, result loginclient.Result) bool {
	if h.nativeWebClientID == "" {
		return false
	}
	if result.Outcome != loginclient.OutcomeRefused || result.Reason != loginclient.RefusalFactorUnsupported {
		return false
	}
	key := false
	for _, method := range result.EnrolledMethods {
		switch method {
		case "AUTHENTICATION_METHOD_TYPE_U2F":
			key = true
		case "AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_IDP":
		default:
			return false
		}
	}
	if !key {
		return false
	}
	target, err := h.client.SecurityKeyLoginURL(c.Request.Context(), requestID, s, h.nativeWebClientID)
	if err != nil {
		h.respondLoginClientError(c, err, "security_key_continuation")
		return true
	}
	body := securityKeyRequiredResponse{}
	body.SecurityKey.URL = target
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "auth.login.security_key_required", "login requires native security key", "auth_request_id", requestID, "outcome", "security_key_required")
	respond.OK(c, body)
	return true
}

// SetNativeWebClientID binds the native-key action to this deployment's OIDC client.
func (h *LoginUIHandlers) SetNativeWebClientID(clientID string) { h.nativeWebClientID = clientID }
