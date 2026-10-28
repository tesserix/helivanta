package testinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TransitPolicy mirrors helivanta-api-transit in tesserix-k8s charts/thirdparty/openbao/values.yaml.
const TransitPolicy = `path "transit/encrypt/helivanta-staff-*" { capabilities = ["create", "update"] }
path "transit/decrypt/helivanta-staff-*" { capabilities = ["update"] }
path "transit/hmac/helivanta-staff-*"    { capabilities = ["update"] }
path "transit/encrypt/helivanta-emergency-*" { capabilities = ["create", "update"] }
path "transit/decrypt/helivanta-emergency-*" { capabilities = ["update"] }
path "transit/hmac/helivanta-emergency-*" { capabilities = ["update"] }
`

var (
	baoOnce  sync.Once
	baoAddr  string
	baoRoot  string
	baoError error
)

// StartOpenBao returns the address and root token of the shared dev-mode OpenBao with transit/ and userpass/ mounted.
func StartOpenBao(t *testing.T) (addr, root string) {
	t.Helper()
	baoOnce.Do(func() { baoAddr, baoRoot, baoError = startOpenBao(t) })
	if baoError != nil {
		t.Fatalf("%v", baoError)
	}
	return baoAddr, baoRoot
}

func startOpenBao(t *testing.T) (string, string, error) {
	t.Helper()
	ctx := context.Background()
	root := uuid.NewString()
	bao, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image:        "openbao/openbao:2.6.2",
		Cmd:          []string{"server", "-dev", "-dev-root-token-id=" + root, "-dev-listen-address=0.0.0.0:8200"},
		ExposedPorts: []string{"8200/tcp"},
		WaitingFor:   wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp").WithStartupTimeout(startupTimeout(t)),
	}})
	if err != nil {
		return "", "", fmt.Errorf("start openbao: %w", err)
	}
	endpoint, err := bao.PortEndpoint(ctx, "8200/tcp", "http")
	if err != nil {
		return "", "", fmt.Errorf("openbao endpoint: %w", err)
	}
	for _, step := range []struct{ path, body string }{
		{"/v1/sys/mounts/transit", `{"type":"transit"}`},
		{"/v1/sys/auth/userpass", `{"type":"userpass"}`},
	} {
		if err := OpenBaoWrite(endpoint, root, http.MethodPost, step.path, step.body); err != nil {
			return "", "", err
		}
	}
	policy, _ := json.Marshal(map[string]string{"policy": TransitPolicy})
	if err := OpenBaoWrite(endpoint, root, http.MethodPut, "/v1/sys/policies/acl/helivanta-api-transit", string(policy)); err != nil {
		return "", "", err
	}
	return endpoint, root, nil
}

// OpenBaoUser creates a userpass identity holding policies whose tokens last ttl.
func OpenBaoUser(t *testing.T, ttl time.Duration, policies ...string) (user, password string) {
	t.Helper()
	addr, root := StartOpenBao(t)
	user, password = "u"+uuid.NewString()[:8], uuid.NewString()
	body, _ := json.Marshal(map[string]any{"password": password, "token_policies": policies, "token_ttl": int(ttl.Seconds()), "token_max_ttl": int(ttl.Seconds())})
	if err := OpenBaoWrite(addr, root, http.MethodPost, "/v1/auth/userpass/users/"+user, string(body)); err != nil {
		t.Fatalf("%v", err)
	}
	return user, password
}

// OpenBaoWrite performs one root-token request and fails on any non-2xx answer.
func OpenBaoWrite(addr, token, method, path, body string) error {
	req, err := http.NewRequest(method, addr+path, bytes.NewBufferString(body)) //nolint:noctx // test setup against a local container
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("openbao %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("openbao %s: status %d", path, resp.StatusCode)
	}
	return nil
}
