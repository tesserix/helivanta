package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRefusesIncompleteCommands(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"publish"},
		{"stage", "-by", "Asha"},
		{"stage", "-file", "x.json"},
		{"decide", "-release", "not-a-uuid", "-by", "Ravi", "-publish", "-reason", "ok"},
		{"decide", "-release", "01a17b70-4da0-7c92-b310-c7186f2d957e", "-by", "Ravi", "-reason", "neither"},
		{"decide", "-release", "01a17b70-4da0-7c92-b310-c7186f2d957e", "-by", "Ravi", "-publish", "-reject", "-reason", "both"},
		{"stage", "-file", "x.json", "-by", "Asha", "extra"},
		{"decide", "-release", "01a17b70-4da0-7c92-b310-c7186f2d957e", "-by", "Ravi", "-publish", "-reason", "no reviewed file"},
	} {
		require.ErrorIs(t, run(t.Context(), args, "postgres://unused", &bytes.Buffer{}), errUsage, args)
	}
	require.ErrorContains(t, run(t.Context(), []string{"stage", "-file", "x.json", "-by", "Asha"}, "", &bytes.Buffer{}), "DRUGMASTER_DATABASE_URL")
}

func TestArchiveRetainsExactApprovalBytesAndDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("reviewed release bytes")
	first, err := archiveRelease(dir, raw)
	require.NoError(t, err)
	second, err := archiveRelease(dir, raw)
	require.NoError(t, err)
	require.Equal(t, first, second)
	saved, err := os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, raw, saved)
	info, err := os.Stat(first)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, os.WriteFile(first, []byte("tampered"), 0600))
	_, err = archiveRelease(dir, raw)
	require.ErrorContains(t, err, "does not match")
}
