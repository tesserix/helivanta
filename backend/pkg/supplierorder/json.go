package supplierorder

import "github.com/tesserix/helivanta/pkg/suppliertrade"

// DecodeObject uses the order contract's bound independently of smaller trade messages.
func DecodeObject(raw []byte, target any) error {
	return suppliertrade.DecodeBoundedObject(raw, target, 64<<10)
}
