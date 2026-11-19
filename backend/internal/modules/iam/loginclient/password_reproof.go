package loginclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"
)

// ErrPasswordChangeUnconfirmed preserves the mandatory-change gate after a native acknowledgement.
var ErrPasswordChangeUnconfirmed = errors.New("loginclient: password change unconfirmed")

// ReprovePasswordAfterChange restores the primary fact cleared by the native
// password-change event. The new secret stays in this request only.
func (c *Client) ReprovePasswordAfterChange(ctx context.Context, session Session, password string) (Session, error) {
	if password == "" || !utf8.ValidString(password) || utf8.RuneCountInString(password) > 200 || session.ID == "" || session.Token == "" {
		return Session{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	subject, err := c.sessionSubject(ctx, session.ID)
	if err != nil {
		return Session{}, err
	}
	state, err := c.passwordState(ctx, subject.UserID)
	if err != nil {
		return Session{}, err
	}
	if state.changeRequired {
		return Session{}, ErrPasswordChangeUnconfirmed
	}
	if !state.changed.After(time.Unix(0, 0)) {
		return Session{}, ErrUnavailable
	}
	boundary := state.changed.Truncate(time.Second).Add(time.Second)
	for check := 0; check < 2; check++ {
		var wire struct {
			SessionToken string `json:"sessionToken"`
		}
		body := map[string]any{"sessionToken": session.Token, "checks": map[string]any{"password": map[string]any{"password": password}}}
		if err := c.do(ctx, http.MethodPatch, "/v2/sessions/"+url.PathEscape(session.ID), body, &wire, ErrBadCredentials); err != nil {
			return Session{}, err
		}
		if wire.SessionToken == "" {
			return Session{}, ErrUnavailable
		}
		session.Token = wire.SessionToken
		proof, err := c.passwordProof(ctx, session)
		if err != nil {
			return Session{}, err
		}
		if ctx.Err() != nil {
			return Session{}, ErrUnavailable
		}
		if !proof.Before(boundary) {
			return session, nil
		}
		if proof.Before(state.changed) || check == 1 {
			return Session{}, ErrUnavailable
		}
		// Never round a native change down to fit OIDC's whole-second auth_time.
		delay := boundary.Sub(proof)
		if delay <= 0 || delay > time.Second {
			return Session{}, ErrUnavailable
		}
		timer := time.NewTimer(delay + time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Session{}, ErrUnavailable
		case <-timer.C:
		}
	}
	return Session{}, ErrUnavailable
}
