package suppliertrade

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// DecodeObject preserves one unambiguous bounded trade intent across owner and peer decoders.
func DecodeObject(raw []byte, target any) error { return DecodeBoundedObject(raw, target, 16<<10) }

// DecodeBoundedObject retains the same ambiguity checks with an explicit owner contract bound.
func DecodeBoundedObject(raw []byte, target any, limit int) error {
	if limit < 1 || limit > 64<<10 || len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalid
	}
	if readObject(d, 1) != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	typed := json.NewDecoder(bytes.NewReader(raw))
	typed.DisallowUnknownFields()
	if typed.Decode(target) != nil || typed.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func readObject(d *json.Decoder, depth int) error {
	if depth > 12 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] || len(seen) >= 64 {
			return ErrInvalid
		}
		seen[name] = true
		if readValue(d, depth+1) != nil {
			return ErrInvalid
		}
	}
	close, err := d.Token()
	if err != nil || close != json.Delim('}') {
		return ErrInvalid
	}
	return nil
}
func readValue(d *json.Decoder, depth int) error {
	if depth > 12 {
		return ErrInvalid
	}
	v, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, compound := v.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		return readObject(d, depth)
	case '[':
		count := 0
		for d.More() {
			count++
			if count > 128 || readValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}
