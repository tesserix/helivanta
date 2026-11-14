package authz

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

const handlerScopeKey = "helivanta.handler_scope_admission"

var errHandlerScopeMissing = errors.New("handler-scoped route responded without current scoped authority")

type handlerScopeAdmission struct {
	tenant, subject, hospital string
	permission                Permission
	admitted                  bool
}

// RequireHandlerScope withholds every non-error response until the handler proves
// current IAM authority for permission at the route's hospital, or a department inside it.
func RequireHandlerScope(permission Permission, hospitalParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, _, ok := authn.ExpectedTenantPrincipal(c)
		if !ok {
			return
		}
		hospital := c.Param(hospitalParam)
		id, err := uuid.Parse(hospital)
		if err != nil || id == uuid.Nil || id.String() != hospital {
			respond.Error(c, http.StatusBadRequest, "invalid_scope", "A valid hospital scope is required.")
			return
		}
		admission := &handlerScopeAdmission{tenant: p.TenantID, subject: p.Subject, hospital: hospital, permission: permission}
		original := c.Writer
		headers := original.Header().Clone()
		writer := &handlerScopeWriter{ResponseWriter: original, admission: admission}
		c.Set(handlerScopeKey, admission)
		c.Writer = writer
		defer func() { c.Writer = original }()
		c.Next()
		c.Writer = original
		if writer.released {
			return
		}
		if admission.admitted || writer.status >= http.StatusBadRequest {
			if writer.status != 0 {
				original.WriteHeader(writer.status)
			}
			return
		}
		// Fail closed: a success the handler never authorized is a defect, never a disclosure.
		for key := range original.Header() {
			original.Header().Del(key)
		}
		for key, values := range headers {
			original.Header()[key] = values
		}
		respond.InternalErr(c, errHandlerScopeMissing, "authorization not proven")
	}
}

// AdmitHandlerScope accepts only the exact IAM-issued evidence for the route's principal, hospital and permission.
func AdmitHandlerScope(ctx context.Context, c *gin.Context, request ScopeRequest) bool {
	value, ok := c.Get(handlerScopeKey)
	if !ok {
		return false
	}
	admission, ok := value.(*handlerScopeAdmission)
	if !ok || request.TenantID != admission.tenant || request.Subject != admission.subject || request.HospitalID != admission.hospital || request.Permission != admission.permission {
		return false
	}
	grant, ok := ScopedCommandGrant(ctx, request)
	if !ok || grant.AssignmentID == "" {
		return false
	}
	admission.admitted = true
	return true
}

// handlerScopeWriter forwards error responses immediately and successes only after admission.
type handlerScopeWriter struct {
	gin.ResponseWriter
	admission *handlerScopeAdmission
	status    int
	released  bool
}

func (w *handlerScopeWriter) release() bool {
	if w.released {
		return true
	}
	if !w.admission.admitted && w.status < http.StatusBadRequest {
		return false
	}
	w.released = true
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	return true
}

func (w *handlerScopeWriter) WriteHeader(code int) {
	if !w.released {
		w.status = code
	}
}

func (w *handlerScopeWriter) Write(data []byte) (int, error) {
	if !w.release() {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *handlerScopeWriter) WriteString(data string) (int, error) {
	if !w.release() {
		return len(data), nil
	}
	return w.ResponseWriter.WriteString(data)
}

func (w *handlerScopeWriter) WriteHeaderNow() {
	if w.release() {
		w.ResponseWriter.WriteHeaderNow()
	}
}

func (w *handlerScopeWriter) Flush() {
	if w.release() {
		w.ResponseWriter.Flush()
	}
}

func (w *handlerScopeWriter) Status() int {
	if !w.released && w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

func (w *handlerScopeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errHandlerScopeMissing
}
