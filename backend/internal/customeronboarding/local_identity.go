package customeronboarding

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

type localIdentityResolver struct{ broker *Handlers }

// NewLocalIdentityResolver connects explicit development runs to the real local identity owner.
func NewLocalIdentityResolver(environment, issuer, enabled string) (*localIdentityResolver, error) {
	if environment != "dev" || issuer != "http://auth.tesserix.localhost:20080" || enabled != "true" {
		return nil, errors.New("local identity owner requires explicit development configuration")
	}
	return &localIdentityResolver{broker: &Handlers{base: "http://127.0.0.1:18787", localIdentityFixture: true, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}, nil
}
func (r *localIdentityResolver) TenantLoginSubject(ctx context.Context, org, email string) (string, error) {
	return r.broker.TenantLoginSubject(ctx, org, email)
}
func (r *localIdentityResolver) TenantIdentitySnapshot(ctx context.Context, tenant, subject string, purpose loginclient.IdentityReadPurpose) (loginclient.IdentitySnapshot, error) {
	return r.broker.TenantIdentitySnapshot(ctx, tenant, subject, purpose)
}
func (h *Handlers) identityCredential() ([]byte, error) {
	if h.localIdentityFixture {
		return []byte("helivanta-local-identity-fixture-only"), nil
	}
	return os.ReadFile(h.tokenFile)
}
