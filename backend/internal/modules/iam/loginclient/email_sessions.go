package loginclient

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// ErrTooManyAccounts means more accounts sit behind an email than one
// submission may check (#1751 spec D9). Checking an arbitrary subset would
// silently lock the person out of the rest, so no password is checked.
var ErrTooManyAccounts = errors.New("loginclient: too many accounts behind this email")

// MaxEmailCandidates bounds the password checks one submission can cause, and
// so how many accounts a wrong password counts against (spec D6).
const MaxEmailCandidates = 5

// LoginTarget is one eligible account the identity owner resolved for an
// email. Name and Account are display data, disclosed only after this
// account's password check succeeded.
type LoginTarget struct{ TenantID, OrganizationID, Subject, Name, Account string }

// TenantTargetResolver is the identity owner's unscoped email lookup
// (onboarding-service#46). truncated reports more than MaxEmailCandidates
// eligible accounts, in which case targets is empty.
type TenantTargetResolver interface {
	TenantLoginTargets(ctx context.Context, email string) (targets []LoginTarget, truncated bool, err error)
}

// SetTenantTargetResolver wires the unscoped lookup without expanding the native login PAT.
func (c *Client) SetTenantTargetResolver(resolver TenantTargetResolver) { c.tenantTargets = resolver }

// TenantSession is a password-verified session. Target is zero for a session
// found by login name rather than through the identity owner.
type TenantSession struct {
	Target  LoginTarget
	Session Session
}

// emailSessions checks password against every account behind email at once
// (spec D2). It returns every accepted session; none is a credential refusal.
func (c *Client) emailSessions(ctx context.Context, email, password string) ([]TenantSession, error) {
	if c.tenantTargets == nil {
		return nil, ErrUnavailable
	}
	targets, truncated, err := c.tenantTargets.TenantLoginTargets(ctx, email)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, ErrTooManyAccounts
	}
	if len(targets) > MaxEmailCandidates {
		return nil, fmt.Errorf("loginclient: identity owner returned %d targets, over its bound: %w", len(targets), ErrUnavailable)
	}
	if len(targets) == 0 {
		// One password check like any other refusal, and a login name that
		// happens to contain "@" still signs in.
		session, err := c.CreatePasswordSession(ctx, email, password)
		if err != nil {
			return nil, err
		}
		return []TenantSession{{Session: session}}, nil
	}

	// Concurrent, so the wall time is one check rather than one per account
	// and the response time does not reveal how many accounts there are.
	sessions := make([]Session, len(targets))
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Go(func() { sessions[i], errs[i] = c.targetSession(ctx, target, password) })
	}
	wg.Wait()

	var accepted []TenantSession
	var refusal, failure error
	for i, err := range errs {
		switch {
		case err == nil:
			accepted = append(accepted, TenantSession{Target: targets[i], Session: sessions[i]})
		case IsCredentialRefusal(err):
			refusal = cmp.Or(refusal, err)
		default:
			failure = cmp.Or(failure, err)
		}
	}
	if failure != nil {
		// Fail closed: an outage or a session outside its organisation on any
		// candidate means this answer cannot be trusted for the others.
		for _, s := range accepted {
			_ = c.DeleteSession(context.WithoutCancel(ctx), s.Session)
		}
		return nil, failure
	}
	if len(accepted) == 0 {
		return nil, refusal
	}
	return accepted, nil
}

// targetSession checks password for exactly target's user and refuses a
// session Zitadel placed in any other organisation.
func (c *Client) targetSession(ctx context.Context, target LoginTarget, password string) (Session, error) {
	session, err := c.createPasswordSession(ctx, map[string]any{"userId": target.Subject}, password)
	if err != nil {
		return Session{}, err
	}
	subject, err := c.sessionSubject(ctx, session.ID)
	if err == nil && (subject.OrgID != target.OrganizationID || subject.UserID != target.Subject) {
		err = ErrAuthRequestInvalid
	}
	if err != nil {
		_ = c.DeleteSession(context.WithoutCancel(ctx), session)
		return Session{}, err
	}
	return session, nil
}

// DeleteSession terminates s in Zitadel (DELETE /v2/sessions/{id}). It is
// best effort: callers have already decided not to use s, and an undeleted
// session is unusable without its token, which only Helivanta holds and
// whose login_attempt row expires. Failures are reported for logging only.
func (c *Client) DeleteSession(ctx context.Context, s Session) error {
	if s.ID == "" || strings.ContainsAny(s.ID, "/?#") {
		return fmt.Errorf("loginclient: refusing to delete session %q: %w", s.ID, ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	return c.do(ctx, http.MethodDelete, "/v2/sessions/"+url.PathEscape(s.ID), map[string]string{"sessionToken": s.Token}, nil, ErrUnavailable)
}
