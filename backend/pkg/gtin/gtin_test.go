package gtin_test

import (
	"testing"

	"github.com/tesserix/helivanta/pkg/gtin"
)

func TestNormalizeValidatesCheckDigitsAndPreservesIdentity(t *testing.T) {
	for _, s := range []string{"96385074", "036000291452", "4006381333931", "04006381333931"} {
		got, ok := gtin.Normalize(s)
		if !ok || len(got) != 14 {
			t.Fatalf("Normalize(%q) = %q, %v", s, got, ok)
		}
	}
	a, _ := gtin.Normalize("4006381333931")
	b, _ := gtin.Normalize("04006381333931")
	if a != b {
		t.Fatal("equivalent GTINs must share one key")
	}
	for _, s := range []string{"", "4006381333932", "400638133393", "４００６３８１３３３９３１", "4006381333931 ", "../4006381333931", "000000000000000"} {
		if _, ok := gtin.Normalize(s); ok {
			t.Errorf("accepted invalid GTIN %q", s)
		}
	}
}
