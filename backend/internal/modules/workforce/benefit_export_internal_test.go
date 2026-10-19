package workforce

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/csvsafe"
)

func TestBenefitCoverageCSVRefusesOversizedEncodingWithoutReturningPartialBytes(t *testing.T) {
	data, err := encodeBenefitCoverageCSV([]benefitCoverageRow{{EmployeeName: strings.Repeat("Synthetic", 1024*1024)}}, "2024-06-01")
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestBenefitCoverageCSVRefusesACoveredDependentItCannotRead(t *testing.T) {
	name, relationship, status := "Synthetic Child", "Child", "active"
	dependent := uuid.MustParse("0199e0b4-0000-7000-8000-000000000001")
	for _, row := range []benefitCoverageRow{
		{DependentID: &dependent, DependentRelationship: &relationship, DependentStatus: &status},
		{DependentID: &dependent, DependentName: &name, DependentStatus: &status},
		{DependentID: &dependent, DependentName: &name, DependentRelationship: &relationship},
	} {
		data, err := encodeBenefitCoverageCSV([]benefitCoverageRow{row}, "2024-06-01")
		require.ErrorIs(t, err, errBenefitDependentUnreadable)
		require.Nil(t, data)
	}
}
