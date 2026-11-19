package customeronboarding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/ratelimit"
)

func recoveryBroker(t *testing.T, clientID string, scopes []string, remote http.HandlerFunc) (*gin.Engine, *atomic.Int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v2/oidc/auth_requests/V2_test" {
			t.Error("unexpected native request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": "V2_test", "clientId": clientID, "scope": scopes}})
	}))
	t.Cleanup(native.Close)
	calls := new(atomic.Int32)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); remote(w, r) }))
	t.Cleanup(owner.Close)
	file := t.TempDir() + "/token"
	if err := os.WriteFile(file, []byte("synthetic-workload"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{origin: "https://helivanta.app", base: owner.URL, client: owner.Client(), tokenFile: file}
	recovery, err := NewPasswordRecoveryHandlers(h, PasswordRecoveryDeps{Requests: loginclient.New(native.URL, "synthetic", native.Client()), ClientID: "web", Limiter: ratelimit.NewMemory(100)})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.POST("/v1/auth/password-recovery", recovery.Start)
	r.GET("/v1/auth/password-recovery", recovery.Status)
	r.POST("/v1/auth/password-recovery/verify", recovery.Verify)
	r.POST("/v1/auth/password-recovery/password", recovery.Password)
	r.POST("/v1/auth/password-recovery/prepare", recovery.Prepare)
	r.POST("/v1/auth/password-recovery/cancel", recovery.Cancel)
	return r, calls
}
func recoveryCall(r http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Origin", "https://helivanta.app")
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRecoveryStartDerivesTenantOnlyFromThisDeploymentNativeRequest(t *testing.T) {
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, client string
		scopes       []string
		status       int
	}{
		{"unscoped", "web", []string{"openid"}, 409}, {"foreign client", "foreign", []string{scope}, 400}, {"duplicate", "web", []string{scope, scope}, 400}, {"domain", "web", []string{"urn:zitadel:iam:org:domain:primary:other.example"}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, calls := recoveryBroker(t, tc.client, tc.scopes, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
			body := `{"auth_request_id":"V2_test","request_id":"` + uuid.Must(uuid.NewV7()).String() + `","email":"person@example.test"}`
			rec := recoveryCall(r, "POST", "/v1/auth/password-recovery", body, nil)
			if rec.Code != tc.status || calls.Load() != 0 {
				t.Fatalf("unproven native scope reached delivery: status=%d calls=%d", rec.Code, calls.Load())
			}
		})
	}
}

func TestRecoveryBrokerKeepsRetryAndVerifiedResendCapabilitiesPrivate(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	original := uuid.Must(uuid.NewV7()).String()
	resend := uuid.Must(uuid.NewV7()).String()
	var requests []map[string]string
	r, _ := recoveryBroker(t, "web", []string{"openid", "urn:zitadel:iam:org:id:helivanta-" + tenant}, func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer synthetic-workload" {
			t.Error("private workload proof missing")
		}
		var body map[string]string
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			t.Error("invalid private handoff")
			w.WriteHeader(500)
			return
		}
		requests = append(requests, body)
		if req.URL.Path != "/internal/v1/customer-signups/password-recovery/start" || body["tenantId"] != tenant || body["email"] != "person@example.test" {
			t.Error("recovery escaped derived tenant scope")
		}
		w.WriteHeader(202)
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "check_email", "challengeId": original})
	})
	body := func(id string) string {
		return `{"auth_request_id":"V2_test","request_id":"` + id + `","email":"person@example.test"}`
	}
	first := recoveryCall(r, "POST", "/v1/auth/password-recovery", body(original), nil)
	if first.Code != 202 || strings.Contains(first.Body.String(), original) || strings.Contains(first.Body.String(), "challengeId") {
		t.Fatal("private capability exposed", first.Code)
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != recoveryCookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Domain != "" || cookies[0].Path != "/" {
		t.Fatal("recovery capability not host-private")
	}
	retry := recoveryCall(r, "POST", "/v1/auth/password-recovery", body(original), cookies[0])
	again := recoveryCall(r, "POST", "/v1/auth/password-recovery", body(resend), cookies[0])
	if retry.Code != 202 || again.Code != 202 || len(requests) != 3 {
		t.Fatal("delivery retry unavailable")
	}
	if requests[0]["id"] != original || requests[1]["id"] != original || requests[2]["id"] != resend || requests[0]["browserHash"] != requests[2]["browserHash"] {
		t.Fatal("retry/resend changed immutable delivery or browser proof")
	}
	restored := again.Result().Cookies()
	if len(restored) != 1 || restored[0].Value != cookies[0].Value {
		t.Fatal("verified resend lost original private capability")
	}
}

func TestRecoveryBrokerRejectsAmbiguousBrowserHeadersAndReadBodiesBeforePrivateIO(t *testing.T) {
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, method string
		types        []string
		body         string
		status       int
	}{
		{"ambiguous content type", "POST", []string{"application/json", "text/plain"}, `{"auth_request_id":"V2_test","request_id":"` + uuid.Must(uuid.NewV7()).String() + `","email":"person@example.test"}`, 415},
		{"status carries command body", "GET", nil, `{"password":"synthetic"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, calls := recoveryBroker(t, "web", []string{scope}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
			cap, err := newCapability()
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(tc.method, "/v1/auth/password-recovery", strings.NewReader(tc.body))
			req.Header.Set("Origin", "https://helivanta.app")
			for _, kind := range tc.types {
				req.Header.Add("Content-Type", kind)
			}
			req.AddCookie(&http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status || calls.Load() != 0 {
				t.Fatalf("ambiguous recovery request crossed the private boundary: status=%d calls=%d", rec.Code, calls.Load())
			}
		})
	}
}

func TestRecoveryBrokerBindsProofAndPasswordWithoutForwardingPrivateCookies(t *testing.T) {
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	var passwordWrites atomic.Int32
	r, _ := recoveryBroker(t, "web", []string{scope}, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]string
		if json.NewDecoder(req.Body).Decode(&body) != nil || body["id"] != cap.id.String() || body["browserHash"] != cap.hash() {
			t.Error("private proof not bound to browser")
		}
		w.Header().Add("Set-Cookie", "helivanta_session=synthetic-must-not-forward; Path=/; Secure; HttpOnly")
		switch req.URL.Path {
		case "/internal/v1/customer-signups/password-recovery/verify":
			if body["code"] != "12345678" {
				t.Error("mailbox proof changed")
			}
			_, _ = w.Write([]byte(`{"state":"verified"}`))
		case "/internal/v1/customer-signups/password-recovery/password":
			passwordWrites.Add(1)
			if body["commandId"] != command || body["password"] != "synthetic-password" {
				t.Error("immutable password handoff changed")
			}
			_, _ = w.Write([]byte(`{"state":"unconfirmed","commandId":"` + command + `"}`))
		case "/internal/v1/customer-signups/password-recovery/status":
			_, _ = w.Write([]byte(`{"state":"unconfirmed","commandId":"` + command + `"}`))
		default:
			t.Error("unexpected private recovery action")
			w.WriteHeader(500)
		}
	})
	cookie := &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}
	for _, tc := range []struct{ method, path, body, state string }{
		{"POST", "/v1/auth/password-recovery/verify", `{"code":"12345678"}`, "verified"},
		{"POST", "/v1/auth/password-recovery/password", `{"command_id":"` + command + `","password":"synthetic-password"}`, "unconfirmed"},
		{"GET", "/v1/auth/password-recovery", "", "unconfirmed"},
	} {
		marker := &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + cap.token}
		rec := recoveryCall(r, tc.method, tc.path, tc.body, cookie, marker)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), tc.state) || strings.Contains(rec.Body.String(), cap.id.String()) || strings.Contains(rec.Body.String(), cap.hash()) || strings.Contains(rec.Body.String(), "commandId") {
			t.Fatal("private proof or session escaped recovery boundary", rec.Code)
		}
	}
	if passwordWrites.Load() != 1 {
		t.Fatal("status read replayed the password write")
	}
}

func TestRecoveryBrokerRejectsMalformedPrivateOutcomes(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"state":"invented_success"}`, `{"state":"completed","state":"unconfirmed"}`, `{"state":"completed","subject":"private-account"}`, strings.Repeat("a", 8193)} {
		r, _ := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(raw)) })
		rec := recoveryCall(r, "GET", "/v1/auth/password-recovery", "", &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token})
		if rec.Code != 503 || strings.Contains(rec.Body.String(), "private-account") || strings.Contains(rec.Body.String(), "invented_success") {
			t.Fatal("malformed native outcome disclosed as success", rec.Code)
		}
	}
}

func TestRecoveryBrokerRejectsBrowserPrivateSelectorsAndUnrelatedCapabilities(t *testing.T) {
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	r, calls := recoveryBroker(t, "web", []string{scope}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	request := `{"auth_request_id":"V2_test","request_id":"` + uuid.Must(uuid.NewV7()).String() + `","email":"person@example.test","tenantId":"11111111-1111-4111-8111-111111111111"}`
	rec := recoveryCall(r, "POST", "/v1/auth/password-recovery", request, nil)
	if rec.Code != 400 || calls.Load() != 0 || !strings.Contains(strings.ToLower(rec.Body.String()), "password recovery") {
		t.Fatal("private selector was accepted or invalid request used the wrong workflow guidance", rec.Code)
	}
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	rec = recoveryCall(r, "POST", "/v1/auth/password-recovery/verify", `{"code":"12345678"}`, &http.Cookie{Name: cookieName, Value: cap.id.String() + "." + cap.token})
	if rec.Code != 404 || calls.Load() != 0 {
		t.Fatal("signup capability authorized password recovery", rec.Code)
	}
}

func TestRecoveryPasswordRequiresPreviouslyPreparedBrowserMarker(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	r, calls := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"state":"unconfirmed"}`)) })
	rec := recoveryCall(r, "POST", "/v1/auth/password-recovery/password", `{"command_id":"`+uuid.Must(uuid.NewV7()).String()+`","password":"synthetic-password"}`, &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token})
	if rec.Code != 409 || calls.Load() != 0 {
		t.Fatalf("unprepared password reached private owner: status=%d calls=%d", rec.Code, calls.Load())
	}
}

func TestRecoveryPreparationRestoresExactCommandAndPreventsResendFromDroppingIt(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	r, calls := recoveryBroker(t, "web", []string{scope}, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]string
		if json.NewDecoder(req.Body).Decode(&body) != nil || body["commandId"] != command || body["id"] != cap.id.String() || body["browserHash"] != cap.hash() {
			t.Error("exact private command not retained")
		}
		if body["password"] != "" {
			t.Error("password reached no-password intent/status")
		}
		_, _ = w.Write([]byte(`{"state":"prepared","commandId":"` + command + `"}`))
	})
	base := &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}
	rec := recoveryCall(r, "POST", "/v1/auth/password-recovery/prepare", `{"command_id":"`+command+`"}`, base)
	if rec.Code != 200 || rec.Body.String() != `{"state":"prepared"}` {
		t.Fatal("prepare did not strip private selector", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != recoveryCommandCookieName || cookies[0].Value != command+"."+cap.token || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Domain != "" {
		t.Fatal("active marker not private")
	}
	status := recoveryCall(r, "GET", "/v1/auth/password-recovery", "", base, cookies[0])
	if status.Code != 200 || status.Body.String() != `{"state":"prepared"}` {
		t.Fatal("specific receipt could not restore after reload", status.Code)
	}
	before := calls.Load()
	resend := recoveryCall(r, "POST", "/v1/auth/password-recovery", `{"auth_request_id":"V2_test","request_id":"`+uuid.Must(uuid.NewV7()).String()+`","email":"person@example.test"}`, base, cookies[0])
	different := recoveryCall(r, "POST", "/v1/auth/password-recovery/prepare", `{"command_id":"`+uuid.Must(uuid.NewV7()).String()+`"}`, base, cookies[0])
	if resend.Code != 409 || different.Code != 409 || calls.Load() != before {
		t.Fatal("unresolved command marker was discarded")
	}
}

func TestRecoveryMarkerIsRetainedBeforeAnUncertainPreparationResponse(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	r, _ := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) })
	rec := recoveryCall(r, "POST", "/v1/auth/password-recovery/prepare", `{"command_id":"`+command+`"}`, &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token})
	cookies := rec.Result().Cookies()
	if rec.Code != 503 || len(cookies) != 1 || cookies[0].Name != recoveryCommandCookieName || cookies[0].Value != command+"."+cap.token || cookies[0].MaxAge <= 0 {
		t.Fatal("uncertain prepare lost its exact recovery marker")
	}
}

func TestRecoveryRejectsForeignDuplicateAndMismatchedCommandMarkers(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	other, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	base := &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}
	valid := &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + cap.token}
	foreign := &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + other.token}
	for _, markers := range [][]*http.Cookie{{foreign}, {valid, valid}, {&http.Cookie{Name: recoveryCommandCookieName, Value: uuid.Must(uuid.NewV7()).String() + "." + cap.token}}} {
		r, calls := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
		rec := recoveryCall(r, "POST", "/v1/auth/password-recovery/password", `{"command_id":"`+command+`","password":"synthetic-password"}`, append([]*http.Cookie{base}, markers...)...)
		if rec.Code != 409 || calls.Load() != 0 {
			t.Fatal("invalid command marker reached owner", rec.Code)
		}
	}
}

func TestRecoveryCancellationAfterReloadUsesMarkerAndClearsOnlyKnownTerminalOutcome(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	for _, state := range []string{"cancelled", "processing", "unconfirmed"} {
		t.Run(state, func(t *testing.T) {
			r, _ := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, req *http.Request) {
				var body map[string]string
				if json.NewDecoder(req.Body).Decode(&body) != nil || body["commandId"] != command || body["id"] != cap.id.String() || body["browserHash"] != cap.hash() {
					t.Error("reload cancellation not bound to retained intent")
				}
				if state != "cancelled" {
					w.WriteHeader(409)
					return
				}
				_, _ = w.Write([]byte(`{"state":"cancelled","commandId":"` + command + `"}`))
			})
			rec := recoveryCall(r, "POST", "/v1/auth/password-recovery/cancel", `{}`, &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}, &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + cap.token})
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != recoveryCommandCookieName {
				t.Fatal("cancellation changed unrelated capability")
			}
			if state == "cancelled" {
				if rec.Code != 200 || cookies[0].MaxAge != -1 || rec.Body.String() != `{"state":"cancelled"}` {
					t.Fatal("known cancellation did not consume active marker", rec.Code)
				}
			} else if rec.Code != 409 || cookies[0].MaxAge <= 0 {
				t.Fatal("uncertain native outcome released marker", rec.Code)
			}
		})
	}
}

func TestRecoveryExactStatusCannotDiscloseAnOlderReceiptOrReady(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	for _, raw := range []string{`{"state":"ready"}`, `{"state":"refused","commandId":"` + uuid.Must(uuid.NewV7()).String() + `"}`, `{"state":"prepared"}`} {
		r, _ := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(raw)) })
		rec := recoveryCall(r, "GET", "/v1/auth/password-recovery", "", &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}, &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + cap.token})
		if rec.Code != 503 || len(rec.Result().Cookies()) != 0 {
			t.Fatal("older or missing receipt replaced exact uncertain command", rec.Code)
		}
	}
}

func TestRecoveryMissingSpecificCommandRemainsRecoverableAfterReload(t *testing.T) {
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	command := uuid.Must(uuid.NewV7()).String()
	r, _ := recoveryBroker(t, "web", []string{"openid"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	rec := recoveryCall(r, "GET", "/v1/auth/password-recovery", "", &http.Cookie{Name: recoveryCookieName, Value: cap.id.String() + "." + cap.token}, &http.Cookie{Name: recoveryCommandCookieName, Value: command + "." + cap.token})
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "password_recovery_command_unknown") || len(rec.Result().Cookies()) != 0 {
		t.Fatal("lost preparation was misrepresented as fresh recovery", rec.Code)
	}
}
