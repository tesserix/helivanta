package workforce

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

// pharmacyAuthority has no scoped IAM fallback, because no pharmacy HR intake template exists (ADR-0063).
func (h *handlers) pharmacyAuthority(c *gin.Context, p authn.Principal, pharmacy pharmacyEstablishment) establishmentAuthority {
	return func(required []authz.Permission, fn func(*gorm.DB) error) error {
		if !authn.Refresh(c) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		permissions, ok := authz.RefreshPermissions(c)
		if !ok {
			return authz.ErrScopedAuthorizationUnavailable
		}
		for _, permission := range required {
			if !permissions.Has(permission) {
				return authz.ErrScopedCommandDenied
			}
		}
		return h.transaction(c.Request.Context(), p, uuid.UUID(pharmacy), func(tx *gorm.DB) error {
			if err := enrollmentScope(tx, p, uuid.UUID(pharmacy)); err != nil {
				return err
			}
			return fn(tx)
		})
	}
}

func (h *handlers) pharmacyEnrollmentAuthority(c *gin.Context, p authn.Principal, pharmacy pharmacyEstablishment) enrollmentAuthority {
	authority := h.pharmacyAuthority(c, p, pharmacy)
	return func(fn func(*gorm.DB, bool) error) error {
		return authority([]authz.Permission{PermEnrollmentManage}, func(tx *gorm.DB) error { return fn(tx, false) })
	}
}

// pharmacyEnrollmentScope admits only the versioned request-context view: a pharmacy has no HR masters and no unversioned client.
func pharmacyEnrollmentScope(c *gin.Context) (authn.Principal, pharmacyEstablishment, uuid.UUID, bool) {
	p, pharmacy, ok := pharmacyStaffScope(c)
	if !ok {
		return p, pharmacy, uuid.Nil, false
	}
	query := c.Request.URL.Query()
	for _, view := range []string{enrollmentProfileMastersQuery, enrollmentQualificationMastersQuery, enrollmentContactMastersQuery} {
		if _, present := query[view]; present {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "This enrollment view is not supported. Refresh and retry.")
			return p, pharmacy, uuid.Nil, false
		}
	}
	if versions := query[enrollmentRequestContextQuery]; len(versions) != 1 || versions[0] != enrollmentRequestContextVersion {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This enrollment view is not supported. Refresh and retry.")
		return p, pharmacy, uuid.Nil, false
	}
	if !enrollmentSession(c) {
		return p, pharmacy, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, pharmacy, uuid.Nil, false
	}
	return p, pharmacy, id, true
}

// respondPharmacyEnrollment answers the request-context document with the pharmacy Staff shape, which carries no hospital_id.
func respondPharmacyEnrollment(c *gin.Context, result enrollment) {
	document := enrollmentResponse(result, false, false, false, true)
	document["staff"] = pharmacyStaffView(result.Staff)
	respond.OK(c, document)
}

func (h *handlers) getPharmacyEnrollment(c *gin.Context) {
	p, pharmacy, id, ok := pharmacyEnrollmentScope(c)
	if !ok {
		return
	}
	result, err := h.readEnrollmentFor(p, pharmacy.establishment(), id, h.pharmacyEnrollmentAuthority(c, p, pharmacy))
	if err != nil {
		respondIntakeError(c, err)
		return
	}
	respondPharmacyEnrollment(c, result)
}

func (h *handlers) putPharmacyEnrollment(c *gin.Context) {
	p, pharmacy, id, ok := pharmacyEnrollmentScope(c)
	if !ok {
		return
	}
	var in enrollmentInput
	if !decodeSized(c, &in, 32768) {
		return
	}
	if in.RequestContextVersion != enrollmentRequestContextVersion {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This protected enrollment request is not supported. Refresh and retry.")
		return
	}
	if !in.normalizeAndValidate(id) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid enrollment fields")
		return
	}
	result, err := h.writeEnrollmentFor(p, pharmacy.establishment(), id, &in, h.pharmacyEnrollmentAuthority(c, p, pharmacy))
	if err != nil {
		respondEnrollmentWriteError(c, err)
		return
	}
	respondPharmacyEnrollment(c, result)
}
