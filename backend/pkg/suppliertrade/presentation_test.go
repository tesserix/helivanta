package suppliertrade

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestEndpointPresentationRejectsMissingUnsafeOrOversizedOwnerNames(t *testing.T) {
	for _, test := range []struct {
		name, organization, location string
		valid                        bool
	}{
		{"named source", "TEST Pharmacy", "TEST Pune Outlet", true},
		{"India scripts", "TEST ଫାର୍ମାସି", "TEST औषधालय", true},
		{"maximum", strings.Repeat("a", 200), strings.Repeat("b", 200), true},
		{"astral maximum", strings.Repeat("😀", 200), "TEST Premise", true},
		{"missing organization", "", "TEST Premise", false},
		{"missing premise", "TEST Supplier", "", false},
		{"padded", " TEST Supplier", "TEST Premise", false},
		{"control", "TEST Supplier", "TEST\nPremise", false},
		{"oversized organization", strings.Repeat("a", 201), "TEST Premise", false},
		{"oversized premise", "TEST Supplier", strings.Repeat("a", 201), false},
		{"astral oversized", strings.Repeat("😀", 201), "TEST Premise", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.valid, ValidEndpointPresentation(EndpointPresentation{OrganizationName: test.organization, LocationName: test.location}))
		})
	}
}
