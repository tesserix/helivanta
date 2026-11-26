package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func decodeObserved(b []byte, format string) ([]byte, int, error) {
	if format != "xml" && format != "tallyprime" {
		return nil, 0, errors.New("observed format must be xml or tallyprime")
	}
	if format == "tallyprime" && (bytes.HasPrefix(b, []byte{0xff, 0xfe}) || bytes.HasPrefix(b, []byte{0xfe, 0xff})) {
		if len(b)%2 != 0 {
			return nil, 0, errors.New("odd UTF-16 evidence length")
		}
		var order binary.ByteOrder = binary.LittleEndian
		if b[0] == 0xfe {
			order = binary.BigEndian
		}
		units := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i < len(b); i += 2 {
			units = append(units, order.Uint16(b[i:i+2]))
		}
		for i := 0; i < len(units); i++ {
			if units[i] >= 0xd800 && units[i] <= 0xdbff {
				if i+1 == len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return nil, 0, errors.New("unpaired UTF-16 high surrogate")
				}
				i++
			} else if units[i] >= 0xdc00 && units[i] <= 0xdfff {
				return nil, 0, errors.New("unpaired UTF-16 low surrogate")
			}
		}
		b = []byte(string(utf16.Decode(units)))
	}
	if !utf8.Valid(b) {
		return nil, 0, errors.New("evidence is not valid UTF-8")
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	count := 0
	if format == "tallyprime" {
		// TallyPrime 7.2 emits an XML-invalid sentinel in these ignored fields.
		// Escape only its exact observed representation; never touch accounting fields.
		for _, field := range []string{"CSTFORMISSUETYPE", "CSTFORMRECVTYPE", "VCHGSTCLASS", "GSTCLASS", "GSTOVRDNISTAXONMRPAPPLICABLE"} {
			old := "<" + field + ">&#4; Not Applicable</" + field + ">"
			count += strings.Count(s, old)
			s = strings.ReplaceAll(s, old, strings.Replace(old, "&#4;", "&amp;#4;", 1))
		}
	}
	return []byte(s), count, nil
}
