package customeronboarding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestNativeMFAAdmissionRequiresExactBoundedOwnerReceipt(t *testing.T) {
	const generation = "019f39c2-1738-7998-b5de-6d4214f614e0"
	input := customercontract.StaffMFAAdmissionInput{Scope: customercontract.StaffMFAAdmissionScope{
		TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-user", GenerationID: generation, RequestID: "11111111-1111-4111-8111-111111111112",
	}, NativeSessionID: "signed-native-session"}
	valid := `{"generationId":"` + generation + `"}`
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"admitted", valid, 200, nil},
		{"foreign_generation", strings.Replace(valid, generation, "019f39c2-1738-7998-b5de-6d4214f614e1", 1), 200, loginclient.ErrUnavailable},
		{"duplicate", strings.Replace(valid, `"generationId":`, `"generationId":"`+generation+`","generationId":`, 1), 200, loginclient.ErrUnavailable},
		{"recased", strings.Replace(valid, "generationId", "GenerationId", 1), 200, loginclient.ErrUnavailable},
		{"disclosed_session", strings.TrimSuffix(valid, "}") + `,"nativeSessionId":"private"}`, 200, loginclient.ErrUnavailable},
		{"null", `null`, 200, loginclient.ErrUnavailable},
		{"second_document", valid + `{}`, 200, loginclient.ErrUnavailable},
		{"oversized", valid + strings.Repeat(" ", 8193), 200, loginclient.ErrUnavailable},
		{"refused_session", `{"error":"private-user"}`, 401, loginclient.ErrBadCredentials},
		{"refused_workload", `{}`, 403, loginclient.ErrBadCredentials},
		{"invisible_command", `{}`, 404, loginclient.ErrBadCredentials},
		{"stale_generation", `{}`, 409, loginclient.ErrBadCredentials},
		{"rate_limited", `{}`, 429, loginclient.ErrUnavailable},
		{"dependency_outage", `{}`, 503, loginclient.ErrUnavailable},
		{"redirect", `{}`, 302, loginclient.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := filepath.Join(t.TempDir(), "workload-token")
			require.NoError(t, os.WriteFile(token, []byte("synthetic-workload"), 0600))
			calls := 0
			owner := &Handlers{base: "https://owner.example.invalid", tokenFile: token, client: &http.Client{Transport: assignmentIdentityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/internal/v1/customer-signups/staff-mfa-recovery/admit", r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				require.Equal(t, "Bearer synthetic-workload", r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var got customercontract.StaffMFAAdmissionInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				require.Equal(t, input, got)
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			receipt, err := owner.AdmitStaffMFASession(t.Context(), input)
			require.Equal(t, 1, calls, "admission is never automatically retried")
			if tc.want == nil {
				require.NoError(t, err)
				require.Equal(t, generation, receipt.GenerationID)
			} else {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, receipt.GenerationID)
				require.NotContains(t, err.Error(), "private-user")
			}
		})
	}
}

func TestNativeMFAAdmissionRejectsInvalidInputAndUnavailableWorkloadBeforeIO(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input customercontract.StaffMFAAdmissionInput
	}{
		{name: "empty"},
		{name: "missing_sid", input: customercontract.StaffMFAAdmissionInput{Scope: customercontract.StaffMFAAdmissionScope{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-user", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0", RequestID: "11111111-1111-4111-8111-111111111112"}}},
		{name: "missing_workload", input: customercontract.StaffMFAAdmissionInput{Scope: customercontract.StaffMFAAdmissionScope{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "synthetic-user", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0", RequestID: "11111111-1111-4111-8111-111111111112"}, NativeSessionID: "signed-native-session"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &Handlers{client: &http.Client{Transport: assignmentIdentityTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid input reached owner")
				return nil, errors.New("unreachable")
			})}}
			_, err := owner.AdmitStaffMFASession(context.Background(), tc.input)
			require.ErrorIs(t, err, loginclient.ErrUnavailable)
		})
	}
	var missing *Handlers
	_, err := missing.AdmitStaffMFASession(t.Context(), customercontract.StaffMFAAdmissionInput{})
	require.ErrorIs(t, err, loginclient.ErrUnavailable)
}
