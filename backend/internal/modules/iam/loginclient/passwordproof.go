package loginclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Native password changes clear the session's primary proof. Read it again at
// finalisation so a later TOTP check cannot replace that missing primary factor.
func (c *Client) passwordProof(ctx context.Context, session Session) (time.Time, error) {
	if session.ID == "" || session.Token == "" {
		return time.Time{}, ErrAuthRequestInvalid
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v2/sessions/"+url.PathEscape(session.ID), nil, &raw, ErrAuthRequestInvalid); err != nil {
		return time.Time{}, err
	}
	root, err := proofObject(raw, "session")
	if err != nil {
		return time.Time{}, err
	}
	value, err := proofObject(root["session"], "id", "factors")
	if err != nil {
		return time.Time{}, err
	}
	var id string
	if json.Unmarshal(value["id"], &id) != nil || id != session.ID {
		return time.Time{}, ErrUnavailable
	}
	factors, err := proofObject(value["factors"], "password")
	if err != nil {
		return time.Time{}, err
	}
	password, present := factors["password"]
	if !present || bytes.Equal(bytes.TrimSpace(password), []byte("null")) {
		return time.Time{}, ErrAuthRequestInvalid
	}
	proof, err := proofObject(password, "verifiedAt")
	if err != nil {
		return time.Time{}, err
	}
	var timestamp string
	if json.Unmarshal(proof["verifiedAt"], &timestamp) != nil {
		return time.Time{}, ErrUnavailable
	}
	verified, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || !verified.After(time.Unix(0, 0)) {
		return time.Time{}, ErrUnavailable
	}
	return verified, nil
}

func proofObject(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, ErrUnavailable
	}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrUnavailable
		}
		name, ok := token.(string)
		if !ok {
			return nil, ErrUnavailable
		}
		if _, exists := values[name]; exists {
			return nil, ErrUnavailable
		}
		for _, key := range keys {
			if strings.EqualFold(strings.ReplaceAll(name, "_", ""), key) && name != key {
				return nil, ErrUnavailable
			}
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrUnavailable
		}
		values[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrUnavailable
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrUnavailable
	}
	return values, nil
}
