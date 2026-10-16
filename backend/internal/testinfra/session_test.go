package testinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const sessionHelperEnv = "HELIVANTA_SESSION_HELPER"

type processSession struct {
	Session   string `json:"session"`
	Container string `json:"container,omitempty"`
}

func runSessionHelper(t *testing.T, mode, override string) processSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionHelper$", "-test.count=1")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TESTCONTAINERS_SESSION_ID=") && !strings.HasPrefix(value, sessionHelperEnv+"=") && !strings.HasPrefix(value, "RYUK_RECONNECTION_TIMEOUT=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, sessionHelperEnv+"="+mode, "RYUK_RECONNECTION_TIMEOUT=500ms")
	if override != "" {
		cmd.Env = append(cmd.Env, "TESTCONTAINERS_SESSION_ID="+override)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if mode == "docker" {
		cmd.Stdout = io.MultiWriter(&out, os.Stdout)
		cmd.Stderr = cmd.Stdout
	}
	err := cmd.Run()
	require.NoError(t, err, "%s", out.String())
	for _, line := range strings.Split(out.String(), "\n") {
		if data, ok := strings.CutPrefix(line, "PROCESS_SESSION="); ok {
			var result processSession
			require.NoError(t, json.Unmarshal([]byte(data), &result))
			require.NotEmpty(t, result.Session)
			return result
		}
	}
	t.Fatalf("helper returned no session: %s", out.String())
	return processSession{}
}

func TestIndependentProcessesOwnDifferentSessions(t *testing.T) {
	first := runSessionHelper(t, "identity", "")
	second := runSessionHelper(t, "identity", "")
	require.NotEqual(t, first.Session, second.Session, "sibling package binaries must not reuse a reaper that can clean up between their runs (#1176)")
}

func TestExplicitSharedSessionIsPreserved(t *testing.T) {
	result := runSessionHelper(t, "identity", "explicit-reaper-regression-session")
	require.Equal(t, "explicit-reaper-regression-session", result.Session)
}

func TestProcessSessionsCleanUpIndependently(t *testing.T) {
	first := runSessionHelper(t, "docker", "")
	second := runSessionHelper(t, "docker", "")
	require.NotEqual(t, first.Session, second.Session)
	require.NotEqual(t, first.Container, second.Container)
	cli, err := testcontainers.NewDockerClientWithOpts(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cli.Close()) })
	for _, result := range []processSession{first, second} {
		require.NotEmpty(t, result.Container)
		require.Eventually(t, func() bool {
			_, err := cli.ContainerInspect(t.Context(), result.Container, client.ContainerInspectOptions{})
			return errdefs.IsNotFound(err)
		}, time.Minute, 200*time.Millisecond, "reaper must remove its process's container after that process exits")
	}
}

func TestSessionHelper(t *testing.T) {
	mode := os.Getenv(sessionHelperEnv)
	if mode == "" {
		t.Skip("subprocess helper")
	}
	result := processSession{Session: testcontainers.SessionID()}
	if mode == "docker" {
		container, err := testcontainers.Run(t.Context(), "nats:2.10-alpine",
			testcontainers.WithExposedPorts("4222/tcp"),
			testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t), wait.ForListeningPort("4222/tcp")),
		)
		require.NoError(t, err)
		result.Container = container.GetContainerID()
	}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	fmt.Printf("PROCESS_SESSION=%s\n", data)
}
