package loginclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
)

// MembershipEligible verifies a matching active human and provider-verified email.
func (c *Client) MembershipEligible(ctx context.Context, subject string) (bool, error) {
	if c == nil || subject == "" {
		return false, fmt.Errorf("membership identity reader missing: %w", ErrUnavailable)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(subject), nil, &raw, ErrUserNotFound); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return false, nil
		}
		return false, errors.Join(ErrUnavailable, err)
	}
	eligible, err := membershipProof(raw, subject)
	if err != nil {
		// Provider payloads can contain private profile data; report no raw proof.
		return false, fmt.Errorf("membership identity proof unavailable: %w", ErrUnavailable)
	}
	return eligible, nil
}

func membershipProof(raw json.RawMessage, subject string) (bool, error) {
	root, err := identityObject(raw, "user")
	if err != nil {
		return false, err
	}
	user, err := identityObject(root["user"], "userId", "state", "human", "machine")
	if err != nil {
		return false, err
	}
	var id, state string
	if err := json.Unmarshal(user["userId"], &id); err != nil || id != subject {
		return false, ErrUnavailable
	}
	if err := json.Unmarshal(user["state"], &state); err != nil {
		return false, ErrUnavailable
	}
	current, err := userState(state)
	if err != nil || !current.IsActive() {
		return false, err
	}
	human, hasHuman := user["human"]
	_, hasMachine := user["machine"]
	if hasHuman && hasMachine {
		return false, ErrUnavailable
	}
	if !hasHuman {
		if hasMachine {
			return false, nil
		}
		return false, ErrUnavailable
	}
	profile, err := identityObject(human, "email")
	if err != nil {
		return false, err
	}
	email, err := identityObject(profile["email"], "email", "isVerified")
	if err != nil {
		return false, err
	}
	verified, present := email["isVerified"]
	if !present {
		return false, nil
	}
	var confirmed bool
	if string(verified) == "null" || json.Unmarshal(verified, &confirmed) != nil {
		return false, ErrUnavailable
	}
	if !confirmed {
		return false, nil
	}
	var address string
	if err := json.Unmarshal(email["email"], &address); err != nil {
		return false, ErrUnavailable
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address || strings.TrimSpace(address) != address {
		return false, ErrUnavailable
	}
	return true, nil
}

func identityObject(raw json.RawMessage, names ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, ErrUnavailable
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrUnavailable
		}
		key, ok := token.(string)
		if !ok {
			return nil, ErrUnavailable
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ErrUnavailable
		}
		for _, name := range names {
			if key != name && normalizePolicyKey(key) == normalizePolicyKey(name) {
				return nil, ErrUnavailable
			}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrUnavailable
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, ErrUnavailable
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrUnavailable
	}
	return fields, nil
}
