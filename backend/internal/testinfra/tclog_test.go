package testinfra

import (
	"bytes"
	stdlog "log"
	"os"
	"strconv"
	"strings"
	"testing"

	tclog "github.com/testcontainers/testcontainers-go/log"
)

// TestTestcontainersLogIsOnWithoutVerbose pins #963's forensics. Without
// this package's init, testcontainers' default logger is a no-op unless the
// binary runs with -v, and CI runs without it.
func TestTestcontainersLogIsOnWithoutVerbose(t *testing.T) {
	l, ok := tclog.Default().(*stdlog.Logger)
	if !ok {
		t.Fatalf("testcontainers' default logger is %T, want the *log.Logger set by init — it is silent on CI otherwise", tclog.Default())
	}
	if l.Writer() != os.Stderr {
		t.Fatalf("testcontainers logs to %v, want os.Stderr (what go test prints for a failing package)", l.Writer())
	}

	// The bytes it produces carry this process's pid and parent pid, which
	// is what tells apart the binaries that share one Ryuk session.
	var buf bytes.Buffer
	probe := stdlog.New(&buf, l.Prefix(), l.Flags())
	probe.Printf("🔥 Reaper obtained from Docker for this test session %s", "abc123")
	got := buf.String()
	for _, want := range []string{
		"pid=" + strconv.Itoa(os.Getpid()),
		"ppid=" + strconv.Itoa(os.Getppid()),
		"Reaper obtained",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log line %q does not contain %q", got, want)
		}
	}
	if l.Flags()&stdlog.Lmicroseconds == 0 {
		t.Fatalf("log flags %b lack Lmicroseconds; lines must line up with Ryuk's own sub-second timestamps", l.Flags())
	}
}
