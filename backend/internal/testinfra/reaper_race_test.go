package testinfra

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// reaperRaceHelperEnv marks a re-executed copy of this test binary as one of
// the racing "package binaries" below.
const reaperRaceHelperEnv = "HELIVANTA_REAPER_RACE_HELPER"

// TestConcurrentBinariesAllConnectToTheReaper is #963's regression test.
//
// `go test ./...` starts several package binaries at once, and they all share
// one Ryuk (the session id is derived from their common parent's pid). The
// first to need a container creates Ryuk; the others find it and REUSE it. In
// testcontainers-go v0.43.0 the reuse path waited only for Docker's port
// proxy, not for Ryuk to be listening, so a binary that reused a just-created
// Ryuk got `Reaper handshake failed: read ack: EOF`. The library only LOGS
// that, and carries on as if connected. Ryuk never counted that binary as a
// client, and pruned its containers as soon as every other binary had exited:
// on CI, the iam binary's Postgres, mid-run.
//
// This reproduces the shape exactly: four copies of this binary, started at
// once under one parent, each starting a container. Measured with this
// harness's shape: v0.43.0 failed the handshake in every reusing process (30
// of 30 over six trials); v0.44.0, which also waits for Ryuk's "Started" log
// line, in none. testinfra's init (tclog.go) makes the library's log visible,
// which is the only place a failed handshake shows.
func TestConcurrentBinariesAllConnectToTheReaper(t *testing.T) {
	if os.Getenv(reaperRaceHelperEnv) == "1" {
		t.Skip("helper process; runs only TestReaperRaceHelper")
	}
	const binaries = 4
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		outs = make([]string, binaries)
		errs = make([]error, binaries)
	)
	for i := 0; i < binaries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestReaperRaceHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(), reaperRaceHelperEnv+"=1")
			out, err := cmd.CombinedOutput()
			mu.Lock()
			outs[i], errs[i] = string(out), err
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	reused := 0
	for i := 0; i < binaries; i++ {
		if errs[i] != nil {
			t.Fatalf("helper %d failed: %v\n%s", i, errs[i], outs[i])
		}
		if strings.Contains(outs[i], "Reaper handshake failed") {
			t.Fatalf("helper %d reused Ryuk before it was listening, so Ryuk never counted it and would prune its containers while it still ran (#963):\n%s", i, outs[i])
		}
		if strings.Contains(outs[i], "Reaper obtained from Docker") {
			reused++
		}
	}
	// Guard the guard: the race only exists on the REUSE path. If no helper
	// reused another's Ryuk, nothing above was exercised.
	if reused == 0 {
		t.Fatalf("no helper reused an existing Ryuk; the race this test exists for was not exercised")
	}
}

// TestReaperRaceHelper is the body each racing copy runs. It is a no-op in
// an ordinary run.
func TestReaperRaceHelper(t *testing.T) {
	if os.Getenv(reaperRaceHelperEnv) != "1" {
		t.Skip("runs only as a helper of TestConcurrentBinariesAllConnectToTheReaper")
	}
	ctx := context.Background()
	_, err := testcontainers.Run(ctx, "nats:2.10-alpine",
		testcontainers.WithExposedPorts("4222/tcp"),
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
			wait.ForListeningPort("4222/tcp").WithStartupTimeout(startupTimeout(t))),
	)
	if err != nil {
		t.Fatalf("start helper container: %v", err)
	}
}
