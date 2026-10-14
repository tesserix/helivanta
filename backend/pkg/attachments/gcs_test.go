package attachments_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
	"google.golang.org/api/option"
)

func fileScope() attachments.ObjectScope {
	return attachments.ObjectScope{TenantID: uuid.New(), HospitalID: uuid.New(), OwnerKind: "staff", OwnerID: uuid.New(), RecordID: uuid.New(), FileID: uuid.New(), Version: 1, UploadedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
}

func TestGCSQuarantineUploadIsBoundedPrivateAndCreateOnly(t *testing.T) {
	scope := fileScope()
	key, err := attachments.ObjectKey(scope)
	require.NoError(t, err)
	const content = "synthetic upload bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/upload/storage/v1/b/helivanta-test-quarantine/o", r.URL.Path)
		require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/related", media)
		parts := multipart.NewReader(r.Body, params["boundary"])
		part, err := parts.NextPart()
		require.NoError(t, err)
		var attrs map[string]any
		require.NoError(t, json.NewDecoder(part).Decode(&attrs))
		require.Equal(t, key, attrs["name"])
		require.Equal(t, "private, no-store", attrs["cacheControl"])
		require.Equal(t, "application/octet-stream", attrs["contentType"])
		part, err = parts.NextPart()
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Equal(t, content, string(data))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bucket": "helivanta-test-quarantine", "name": key, "generation": "12345", "size": strconv.Itoa(len(content)), "crc32c": strings.TrimPrefix(crcHeader(content), "crc32c=")})
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	object, err := store.PutQuarantine(t.Context(), scope, []byte(content))
	require.NoError(t, err)
	require.EqualValues(t, 12345, object.Generation)
	require.Len(t, object.SHA256, 64)
	for _, bytes := range [][]byte{nil, {}, make([]byte, attachments.MaxFileBytes+1)} {
		_, err := store.PutQuarantine(t.Context(), scope, bytes)
		require.Error(t, err)
	}
}

func crcHeader(content string) string {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], crc32.Checksum([]byte(content), crc32.MakeTable(crc32.Castagnoli)))
	return "crc32c=" + base64.StdEncoding.EncodeToString(bytes[:])
}

func TestGCSReadsOnlyExactScopedQuarantineGeneration(t *testing.T) {
	scope := fileScope()
	key, err := attachments.ObjectKey(scope)
	require.NoError(t, err)
	const content = "synthetic document bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/storage/v1/b/helivanta-test-quarantine/o/"+key, r.URL.Path)
		require.Equal(t, "12345", r.URL.Query().Get("generation"))
		if r.URL.Query().Get("alt") != "media" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": key, "bucket": "helivanta-test-quarantine", "generation": "12345", "size": strconv.Itoa(len(content)), "crc32c": strings.TrimPrefix(crcHeader(content), "crc32c="), "contentType": "text/plain"})
			return
		}
		w.Header().Set("X-Goog-Generation", "12345")
		w.Header().Set("X-Goog-Metageneration", "1")
		w.Header().Set("X-Goog-Hash", crcHeader(content))
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	data, err := store.ReadQuarantine(t.Context(), scope, 12345)
	require.NoError(t, err)
	require.Equal(t, []byte(content), data.Bytes)
	require.EqualValues(t, 12345, data.Generation)
	require.Len(t, data.SHA256, 64)
	require.EqualValues(t, len(content), data.Size)
	for _, generation := range []int64{0, -1} {
		_, err := store.ReadQuarantine(context.Background(), scope, generation)
		require.Error(t, err)
	}
	_, err = store.ReadQuarantine(t.Context(), attachments.ObjectScope{}, 12345)
	require.Error(t, err)
	require.NotContains(t, data.String(), content)
	require.NotContains(t, data.String(), strings.TrimSpace(content))
}

func TestGCSRejectsSubstitutedGenerationAndIntegrity(t *testing.T) {
	for _, tc := range []struct{ name, generation, size, crc, encoding string }{
		{"wrong_generation", "99", "9", strings.TrimPrefix(crcHeader("synthetic"), "crc32c="), ""},
		{"wrong_crc", "12345", "9", "AAAAAA==", ""},
		{"oversize", "12345", strconv.FormatInt(attachments.MaxFileBytes+1, 10), "AAAAAA==", ""},
		{"encoded", "12345", "9", strings.TrimPrefix(crcHeader("synthetic"), "crc32c="), "gzip"},
		{"empty", "12345", "0", "AAAAAA==", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := fileScope()
			key, err := attachments.ObjectKey(scope)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("alt") == "media" {
					w.Header().Set("X-Goog-Generation", tc.generation)
					w.Header().Set("X-Goog-Hash", crcHeader("synthetic"))
					_, _ = w.Write([]byte("synthetic"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"name": key, "bucket": "helivanta-test-quarantine", "generation": tc.generation, "size": tc.size, "crc32c": tc.crc, "contentEncoding": tc.encoding})
			}))
			t.Cleanup(server.Close)
			client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			store, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
			require.NoError(t, err)
			data, err := store.ReadQuarantine(t.Context(), scope, 12345)
			require.Error(t, err)
			require.Empty(t, data.Bytes)
		})
	}
}
