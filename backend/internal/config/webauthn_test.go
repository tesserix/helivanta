package config

import "testing"

func TestValidatedWebAuthnRPID(t *testing.T) {
	cases := []struct {
		name, env, rpID string
		wantErr         bool
	}{
		{"prod exact", "prod", "helivanta.app", false},
		{"prod empty", "prod", "", true},
		{"prod dev value", "prod", "helivanta.localhost", true},
		{"prod auth host", "prod", "auth.tesserix.app", true},
		{"prod with port", "prod", "helivanta.app:443", true},
		{"prod with scheme", "prod", "https://helivanta.app", true},
		{"dev exact", "dev", "helivanta.localhost", false},
		{"dev prod value", "dev", "helivanta.app", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Config{Env: tc.env, WebAuthnRPID: tc.rpID}.ValidatedWebAuthnRPID()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.rpID {
				t.Fatalf("got %q, want %q", got, tc.rpID)
			}
			if tc.wantErr && got != "" {
				t.Fatalf("returned %q alongside an error", got)
			}
		})
	}
}
