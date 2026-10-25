package mediconnectclaims

// FromEnv leaves the owner client disabled unless an explicit complete configuration is supplied.
func FromEnv(get func(string) string) (*Client, error) {
	origin := get("MEDICONNECT_CLAIM_ORIGIN")
	token := get("MEDICONNECT_CLAIM_TOKEN_FILE")
	mode := get("MEDICONNECT_CLAIM_TRANSPORT")
	if origin == "" && token == "" && mode == "" {
		return nil, nil
	}
	return New(origin, token, mode, nil)
}
