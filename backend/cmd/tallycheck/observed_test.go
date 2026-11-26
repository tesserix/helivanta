package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16Evidence(s string) []byte {
	b := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func TestTallyPrimeObservedPreservesAccountingAndScopesControlMarkers(t *testing.T) {
	s := strings.Replace(sampleVoucher, "<DATE>", "<GSTCLASS>&#4; Not Applicable</GSTCLASS><DATE>", 1)
	b, markers, err := decodeObserved(utf16Evidence(s), "tallyprime")
	if err != nil || markers != 1 {
		t.Fatalf("decode: markers=%d err=%v", markers, err)
	}
	actual, err := readVouchers(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	want, err := readVouchers(strings.NewReader(sampleVoucher))
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(want, actual); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"ledger control":          utf16Evidence(strings.ReplaceAll(sampleVoucher, "Purchases", "&#4; Not Applicable")),
		"other control":           utf16Evidence(strings.Replace(s, "&#4;", "&#5;", 1)),
		"unexpected marker value": utf16Evidence(strings.Replace(s, "Not Applicable", "Other", 1)),
		"malformed surrogate":     {0xff, 0xfe, 0x00, 0xd8},
		"odd utf16":               {0xff, 0xfe, 0x41},
	} {
		t.Run(name, func(t *testing.T) {
			decoded, _, err := decodeObserved(input, "tallyprime")
			if err == nil {
				_, err = readVouchers(bytes.NewReader(decoded))
			}
			if err == nil {
				t.Fatal("unsafe observed evidence accepted")
			}
		})
	}
	if _, _, err := decodeObserved(utf16Evidence(s), "xml"); err == nil {
		t.Fatal("Tally-specific input accepted without explicit format")
	}
}
