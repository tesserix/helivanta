package attachments_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func syntheticPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 50, G: 100, B: 150, A: 255})
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func pngMetadata(t *testing.T, kind string, payload []byte) []byte {
	t.Helper()
	data := syntheticPNG(t)
	var chunk bytes.Buffer
	require.NoError(t, binary.Write(&chunk, binary.BigEndian, uint32(len(payload))))
	chunk.WriteString(kind)
	chunk.Write(payload)
	require.NoError(t, binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes()[4:])))
	return append(append(bytes.Clone(data[:len(data)-12]), chunk.Bytes()...), data[len(data)-12:]...)
}

func jpegMetadata(t *testing.T, marker byte, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	data := buffer.Bytes()
	segment := []byte{0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	return append(append(bytes.Clone(data[:2]), segment...), data[2:]...)
}

func TestContentPolicyRejectsOpaqueAndBinaryImageMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		mime string
	}{
		{"private_png_chunk", pngMetadata(t, "teSt", []byte("PK\x03\x04synthetic")), "image/png"},
		{"compressed_png_text", pngMetadata(t, "zTXt", []byte("Note\x00\x00synthetic")), "image/png"},
		{"png_color_profile", pngMetadata(t, "iCCP", []byte("Profile\x00\x00synthetic")), "image/png"},
		{"binary_png_text", pngMetadata(t, "tEXt", []byte("Note\x00PK\x03\x04synthetic")), "image/png"},
		{"oversized_png_text", pngMetadata(t, "tEXt", append([]byte("Note\x00"), bytes.Repeat([]byte("a"), 65537)...)), "image/png"},
		{"opaque_jpeg_app", jpegMetadata(t, 0xe1, []byte("Exif\x00\x00synthetic")), "image/jpeg"},
		{"jpeg_color_profile", jpegMetadata(t, 0xe2, []byte("ICC_PROFILE\x00synthetic")), "image/jpeg"},
		{"binary_jpeg_comment", jpegMetadata(t, 0xfe, []byte("PK\x03\x04synthetic")), "image/jpeg"},
		{"opaque_jpeg_header", jpegMetadata(t, 0xe0, []byte("PK\x03\x04synthetic")), "image/jpeg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, decodeErr := image.Decode(bytes.NewReader(tc.data))
			require.NoError(t, decodeErr, "fixture must be a complete decodable image")
			contentType, err := attachments.InspectContent(tc.data, tc.mime)
			require.Error(t, err)
			require.Empty(t, contentType)
		})
	}
}

func TestContentPolicyAllowsValidatedPassiveMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		mime string
	}{
		{"png_text", pngMetadata(t, "tEXt", []byte("Note\x00Synthetic evidence only")), "image/png"},
		{"jpeg_comment", jpegMetadata(t, 0xfe, []byte("Synthetic evidence only")), "image/jpeg"},
		{"jpeg_jfif", jpegMetadata(t, 0xe0, []byte{'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}), "image/jpeg"},
		{"jpeg_adobe", jpegMetadata(t, 0xee, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0}), "image/jpeg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType, err := attachments.InspectContent(tc.data, tc.mime)
			require.NoError(t, err)
			require.Equal(t, tc.mime, contentType)
		})
	}
}

func TestContentPolicyRequiresSupportedMatchingCompleteImage(t *testing.T) {
	data := syntheticPNG(t)
	contentType, err := attachments.InspectContent(data, "image/png")
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	var jpegBytes bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	contentType, err = attachments.InspectContent(jpegBytes.Bytes(), "image/jpeg")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", contentType)
	for _, tc := range []struct {
		name string
		data []byte
		mime string
	}{
		{"empty", nil, "image/png"},
		{"type_mismatch", data, "image/jpeg"},
		{"script_disguised_as_image", []byte("<script>synthetic</script>"), "image/png"},
		{"truncated", data[:len(data)-6], "image/png"},
		{"appended_content", append(bytes.Clone(data), []byte("<script>synthetic</script>")...), "image/png"},
		{"unsupported_pdf", []byte("%PDF-1.4 synthetic"), "application/pdf"},
		{"archive", []byte("PK\x03\x04synthetic"), "application/zip"},
		{"encoded_mime", data, "image/png;charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType, err := attachments.InspectContent(tc.data, tc.mime)
			require.Error(t, err)
			require.Empty(t, contentType)
		})
	}
}
