package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"
)

func TestRunRefusesIncompleteCommands(t *testing.T) {
	id := "01a17b70-4da0-7c92-b310-c7186f2d957e"
	for _, args := range [][]string{
		nil,
		{"approve"},
		{"list", "extra"},
		{"list", "-by", "Sam"},
		{"decide", "-declaration", "not-a-uuid", "-by", "Sam", "-approve", "-reason", "ok"},
		{"decide", "-declaration", id, "-by", "Sam", "-reason", "neither"},
		{"decide", "-declaration", id, "-by", "Sam", "-approve", "-reject", "-reason", "both"},
		{"decide", "-declaration", id, "-approve", "-reason", "nobody"},
		{"decide", "-declaration", id, "-by", "Sam", "-approve"},
	} {
		require.ErrorIs(t, run(t.Context(), args, "postgres://unused", &bytes.Buffer{}), errUsage, args)
	}
	require.ErrorContains(t, run(t.Context(), []string{"list"}, "", &bytes.Buffer{}), "SCHEDULE_REVIEW_DATABASE_URL")
}

func TestListPrintsWhatADecisionNeeds(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printLowerings(&out, []pharmacy.ScheduleLowering{{
		DeclarationID: uuid.MustParse("01a17b70-4da0-7c92-b310-c7186f2d957e"), ProductName: "TEST Amoxil",
		CurrentClass: "H1", RequestedClass: "none", CurrentNDPS: true, Source: "TEST pack label",
		DeclaredBy: "user-pharmacist", CountersignedBy: "user-owner", CountersignReason: "label misread", DeclaredAt: time.Now(),
	}}))
	for _, want := range []string{"01a17b70-4da0-7c92-b310-c7186f2d957e", "TEST Amoxil", "H1+NDPS", "none", "TEST pack label", "user-pharmacist", "user-owner", "label misread"} {
		require.Contains(t, out.String(), want)
	}
}
