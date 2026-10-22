package tax

import (
	"errors"
	"regexp"
	"strings"

	"github.com/tesserix/helivanta/pkg/countryprofile"
)

// Errors returned by ValidateRegistration. A passing check proves the
// identifier is well formed, not that GSTN has it on record.
var (
	ErrKind                 = errors.New("tax: identifier kind is not the scheme's registration kind")
	ErrFormat               = errors.New("tax: identifier format")
	ErrChecksum             = errors.New("tax: identifier check character")
	ErrJurisdictionMismatch = errors.New("tax: identifier does not belong to the jurisdiction")
)

// State code, PAN (5 letters, 4 digits, 1 letter), entity 1-9/A-Z, Z, check.
var gstinShape = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

const base36 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// NormalizeRegistration trims and upper-cases an identifier as typed.
func NormalizeRegistration(identifier string) string {
	return strings.ToUpper(strings.TrimSpace(identifier))
}

// ValidateRegistration checks a registration against the scheme: its kind,
// its shape, its check character and that it belongs to the jurisdiction.
func ValidateRegistration(s countryprofile.TaxScheme, jurisdiction, kind, identifier string) error {
	if kind != s.RegistrationKind || s.RegistrationKind != "gstin" {
		return ErrKind
	}
	id := NormalizeRegistration(identifier)
	if !gstinShape.MatchString(id) {
		return ErrFormat
	}
	if id[14] != gstinCheck(id[:14]) {
		return ErrChecksum
	}
	if _, ok := s.Jurisdiction(jurisdiction); !ok || id[:2] != jurisdiction {
		return ErrJurisdictionMismatch
	}
	return nil
}

// gstinCheck is GSTN's Luhn mod-36 check character over the first 14
// characters: weights 1,2,1,2… from the left, each product folded as
// v/36 + v%36 (as python-stdnum's stdnum.in_.gstin).
func gstinCheck(body string) byte {
	sum := 0
	for i := range len(body) {
		v := strings.IndexByte(base36, body[i]) * (1 + i%2)
		sum += v/36 + v%36
	}
	return base36[(36-sum%36)%36]
}
