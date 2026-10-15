package attachments

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"unicode"
	"unicode/utf8"
)

// InspectContent admits complete bounded PNG/JPEG evidence, never active formats.
func InspectContent(data []byte, declared string) (string, error) {
	if len(data) < 1 || int64(len(data)) > MaxFileBytes || (declared != "image/png" && declared != "image/jpeg") || http.DetectContentType(data) != declared {
		return "", errors.New("document content type is unsupported or mismatched")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		return "", errors.New("document image dimensions are invalid")
	}
	if (format == "png" && !completePNG(data)) || (format == "jpeg" && !completeJPEG(data)) || (format != "png" && format != "jpeg") {
		return "", errors.New("document image structure is incomplete")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return "", errors.New("document image content is invalid")
	}
	return declared, nil
}

func completePNG(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	for offset := 8; offset < len(data); {
		if len(data)-offset < 12 {
			return false
		}
		rawSize := binary.BigEndian.Uint32(data[offset : offset+4])
		if rawSize > 10<<20 {
			return false
		}
		size := int(rawSize)
		if size > len(data)-offset-12 {
			return false
		}
		typ := string(data[offset+4 : offset+8])
		payload := data[offset+8 : offset+8+size]
		offset += size + 12
		if typ == "IEND" {
			return size == 0 && offset == len(data)
		}
		// Opaque metadata is not rendered image evidence and may evade antivirus inspection.
		if !passivePNGChunk(typ, payload) {
			return false
		}
	}
	return false
}

func plainImageText(data []byte) bool {
	if len(data) > 65536 || !utf8.Valid(data) {
		return false
	}
	for _, char := range string(data) {
		if unicode.IsControl(char) && char != '\t' && char != '\n' && char != '\r' {
			return false
		}
	}
	return true
}

func passivePNGChunk(kind string, data []byte) bool {
	switch kind {
	case "IHDR", "PLTE", "IDAT", "tRNS":
		return true
	case "gAMA":
		return len(data) == 4
	case "cHRM":
		return len(data) == 32
	case "sRGB":
		return len(data) == 1 && data[0] <= 3
	case "pHYs":
		return len(data) == 9 && data[8] <= 1
	case "sBIT":
		return len(data) >= 1 && len(data) <= 4
	case "bKGD":
		return len(data) == 1 || len(data) == 2 || len(data) == 6
	case "tEXt":
		keyword, text, found := bytes.Cut(data, []byte{0})
		if !found || len(keyword) < 1 || len(keyword) > 79 {
			return false
		}
		for _, char := range keyword {
			if char < 32 || char > 126 {
				return false
			}
		}
		return plainImageText(text)
	default:
		return false
	}
}

func passiveJPEGSegment(marker byte, data []byte) bool {
	switch marker {
	case 0xe0:
		return len(data) >= 14 && bytes.Equal(data[:5], []byte("JFIF\x00")) &&
			data[5] == 1 && data[6] <= 2 && data[7] <= 2 &&
			len(data) == 14+3*int(data[12])*int(data[13])
	case 0xee:
		return len(data) == 12 && bytes.Equal(data[:5], []byte("Adobe")) && data[11] <= 2
	case 0xfe:
		return plainImageText(data)
	default:
		return marker < 0xe0 || marker > 0xef
	}
}

func completeJPEG(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return false
	}
	for offset := 2; offset < len(data); {
		if data[offset] != 0xff {
			return false
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset == len(data) {
			return false
		}
		marker := data[offset]
		offset++
		if marker == 0xd9 {
			return offset == len(data)
		}
		if marker == 0 || marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) || len(data)-offset < 2 {
			return false
		}
		size := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if size < 2 || size > len(data)-offset {
			return false
		}
		if !passiveJPEGSegment(marker, data[offset+2:offset+size]) {
			return false
		}
		offset += size
		if marker != 0xda {
			continue
		}
		for offset < len(data) {
			if data[offset] != 0xff {
				offset++
				continue
			}
			start := offset
			for offset < len(data) && data[offset] == 0xff {
				offset++
			}
			if offset == len(data) {
				return false
			}
			if data[offset] == 0 || (data[offset] >= 0xd0 && data[offset] <= 0xd7) {
				offset++
				continue
			}
			offset = start
			break
		}
	}
	return false
}
