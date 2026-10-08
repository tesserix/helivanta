package testinfra

import (
	"fmt"
	stdlog "log"
	"os"

	tclog "github.com/testcontainers/testcontainers-go/log"
)

// testcontainers' own log is turned ON for every test binary that uses this
// package, not only under `go test -v` (#963).
//
// It records what decides whether a container outlives the binary that
// started it:
//   - whether this process CREATED Ryuk or FOUND an existing one;
//   - when each container was created;
//   - when this process connected to Ryuk.
//
// Ryuk prunes the whole session once its client count has sat at zero for
// 10s. On CI the iam binary's Postgres was pruned while iam was still
// running, and Ryuk's log shows no connection from iam alive at the time.
// The library is silent without -v, so nothing recorded what iam's process
// did. Each line is prefixed with the pid and parent pid, because every
// package binary in one `go test ./...` shares one Ryuk session (the session
// id is derived from the parent pid) and the lines have to be told apart.
//
// Cost: none on a green run. `go test` prints a package binary's output only
// when that package fails (or with -v), which is exactly when it is wanted.
func init() {
	tclog.SetDefault(stdlog.New(os.Stderr, processLogPrefix(), stdlog.LstdFlags|stdlog.Lmicroseconds))
}

// processLogPrefix names this test binary in each testcontainers log line.
func processLogPrefix() string {
	return fmt.Sprintf("testcontainers[pid=%d ppid=%d] ", os.Getpid(), os.Getppid())
}
