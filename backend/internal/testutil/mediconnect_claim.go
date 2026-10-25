package testutil

import (
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/modules/mediconnect"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
)

// MediConnectClaimHandler exposes the real owner to cross-owner HTTP integration fixtures.
func MediConnectClaimHandler(db *tenantdb.DB, verifier *workloadidentity.Verifier) gin.HandlerFunc {
	return mediconnect.NewClaimCheckHandler(platform.Deps{DB: db}, verifier)
}
