package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/audit"
)

func TestLogTagsTheRecordAsAnAuditEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	audit.Log(context.Background(), logger, slog.LevelWarn, "auth.login.refused", "login refused", "subject", "u-1")

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	require.Equal(t, "auth.login.refused", rec[audit.LogKey])
	require.Equal(t, "login refused", rec["msg"])
	require.Equal(t, "WARN", rec["level"])
	require.Equal(t, "u-1", rec["subject"])
}
