package attachments_test

import (
	"bytes"
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
