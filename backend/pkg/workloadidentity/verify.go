// Package workloadidentity checks short-lived, audience-bound Kubernetes callers.
package workloadidentity

import (
	"context"
	"errors"
	"strings"
	"time"

	authentication "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authclient "k8s.io/client-go/kubernetes/typed/authentication/v1"
	"k8s.io/client-go/rest"
)

// ErrRejected means the credential did not identify the configured caller.
var ErrRejected = errors.New("workload credential rejected")

// ErrUnavailable means Kubernetes could not evaluate the credential.
var ErrUnavailable = errors.New("workload identity unavailable")

type Verifier struct {
	client            authclient.AuthenticationV1Interface
	audience, account string
}

func New(config *rest.Config, audience, account string) (*Verifier, error) {
	if config == nil || audience == "" || !strings.HasPrefix(account, "system:serviceaccount:") {
		return nil, errors.New("invalid workload identity configuration")
	}
	c := rest.CopyConfig(config)
	c.ContentType = "application/json"
	c.AcceptContentTypes = "application/json"
	c.Timeout = 5 * time.Second
	client, err := authclient.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	return &Verifier{client: client, audience: audience, account: account}, nil
}
func (v *Verifier) Verify(ctx context.Context, authorization string) error {
	if len(authorization) > 8192 || !strings.HasPrefix(authorization, "Bearer ") {
		return ErrRejected
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	if token == "" || strings.ContainsAny(token, " \r\n\t") {
		return ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	review, err := v.client.TokenReviews().Create(ctx, &authentication.TokenReview{Spec: authentication.TokenReviewSpec{Token: token, Audiences: []string{v.audience}}}, metav1.CreateOptions{})
	if err != nil {
		return ErrUnavailable
	}
	if review == nil || !review.Status.Authenticated || review.Status.Error != "" || review.Status.User.Username != v.account || review.Status.User.UID == "" {
		return ErrRejected
	}
	for _, audience := range review.Status.Audiences {
		if audience == v.audience {
			return nil
		}
	}
	return ErrRejected
}

// Subject identifies the exact configured caller accepted by Verify.
func (v *Verifier) Subject() string { return v.account }
